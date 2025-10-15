package suresql

import (
	"fmt"
	"strings"
	"time"

	orm "github.com/medatechnology/simpleorm"

	utils "github.com/medatechnology/goutil"
	"github.com/medatechnology/goutil/medattlmap"
	"github.com/medatechnology/goutil/metrics"
	"github.com/medatechnology/goutil/object"
	"github.com/medatechnology/goutil/print"
	"github.com/medatechnology/goutil/simplelog"
)

const (
	SURESQL_ENV_FILE = ".env.suresql"
	APP_NAME         = "SureSQL"
	APP_VERSION      = "0.0.1"
	// DB_INITIALIZED               = "DB already initialized"
)

// Just for debugging, pingpong function
func PingPong() string {
	return APP_NAME + " " + APP_VERSION + " is running"
}

// Check if pool is enabled, and max pool has not reached
func (n SureSQLNode) IsPoolAvailable() bool {
	if n.IsPoolEnabled && n.DBConnections.Len() < n.MaxPool {
		return true
	}
	return false
}

// Get the DB connection from pool based on token
func (n SureSQLNode) GetDBConnectionByToken(token string) (SureSQLDB, error) {
	var db SureSQLDB
	if CurrentNode.IsPoolEnabled {
		// Get DBConnection based on token
		dbInterface, ok := CurrentNode.DBConnections.Get(token)
		if !ok {
			return db, ErrNoDBConnection
		}
		db = dbInterface.(SureSQLDB)
	} else {
		db = CurrentNode.InternalConnection
	}
	return db, nil
}

// rename the key for DB connection pool to use new token, this is usually because refresh token.
// TODO: please don't use this anymore, when token is refreshed, the DB connection should be deleted
// -     and re-create it again fresh with new expiration same with the token expiration.
func (n *SureSQLNode) RenameDBConnection(old, new string) {
	if val, ok := n.DBConnections.Get(old); ok {
		n.DBConnections.Put(new, 0, val)
		n.DBConnections.Delete(old)
	}
}

// This should be run the first time this package got imported, which is
// connecting to the DB locally / internally. Not yet used by the client.
func ConnectInternal() error {
	// Set the global variable for when server is started from making the DBMS connection
	ServerStartTime = time.Now()

	// IMPROVE: Change this maybe reading from environment or settings table!
	// CurrentNode.IsPoolEnabled = DEFAULT_POOL_ENABLED
	// CurrentNode.MaxPool = DEFAULT_MAX_POOL

	el := metrics.StartTimeIt("Loading environment...", 0)
	utils.ReloadEnvEach(".env.dev", SURESQL_ENV_FILE)
	metrics.StopTimeItPrint(el, "Done")

	el = metrics.StartTimeIt("Loading DBMS config... ", 0)
	conf := LoadDBMSConfigFromEnvironment()
	metrics.StopTimeItPrint(el, "Done")

	// conf.PrintDebug(false)
	el = metrics.StartTimeIt("Making internal connection to DB...", 0)
	db, err := NewDatabase(conf)
	if err != nil {
		simplelog.LogErrorAny("Main", err, "Failed to connect to database")
		return err
	}
	// Internal connection is used by the SureSQL Backend only
	CurrentNode.InternalConnection = db
	CurrentNode.InternalConfig = conf
	// Preparing the DBPool connection that is called by the Handler /connect
	metrics.StopTimeItPrint(el, "Done")

	db_is_initialized := true
	el = metrics.StartTimeIt("Reading config table...", 0)
	err = LoadConfigFromDB(&CurrentNode.InternalConnection)
	if err != nil {
		simplelog.LogErrorStr("init", err, "cannot load settings from DB, it is not yet initialized")
//		return err
		db_is_initialized = false
	}
	metrics.StopTimeItPrint(el, "Done")

	// Init DB is done after LoadSettings just in case if settings already initialized??
	if !db_is_initialized {
		el = metrics.StartTimeIt("Initializing DB tables...", -1)
		err = InitDB(false)
		if err == nil {
			// if no error that means DB is initalized, if it's already initialized it will return err=ErrDBInitializedAlready
			// call the LoadSEttings again
			err := LoadConfigFromDB(&CurrentNode.InternalConnection)
			if err != nil {
				simplelog.LogErrorStr("connect internal", err, "cannot load settings from DB, it is not yet initialized")
				return err
			}
		} else {
			metrics.StopTimeItPrint(el, err.Error())
		}
		metrics.StopTimeItPrint(el, "Done")
	}

	// Make the configMaps before reading from DB
	CurrentNode.Settings = make(Settings)

	el = metrics.StartTimeIt("Reading settings table...", 0)
	err = LoadSettingsFromDB(&CurrentNode.InternalConnection)
	if err != nil {
		simplelog.LogErrorStr("init", err, "cannot load configs from DB or not yet initialized")
		return err
	}
	metrics.StopTimeItPrint(el, "Done")

	el = metrics.StartTimeIt("Reading DBMS status...", 0)
	_, err = GetStatusInternal(CurrentNode.InternalConnection, NODE_MODE)
	if err != nil {
		simplelog.LogErrorStr("init", err, "cannot get status from DB")
		return err
	}
	metrics.StopTimeItPrint(el, "Done")

	// Setup the DB Connection TTLMap, use RefreshTokenExp (longer) so when refreshed, the DBConnection is still there.
	el = metrics.StartTimeIt("Applying config table and settings to Node status...", 0)
	CurrentNode.ApplyAllConfig()
	CurrentNode.DBConnections = medattlmap.NewTTLMap(CurrentNode.Config.RefreshExp, CurrentNode.Config.TTLTicker)
	CurrentNode.GetStatusFromSettings(conf)
	metrics.StopTimeItPrint(el, "Done")

	// QUESTION: Just to be safe, put the pool that we get from this node * number of peers
	// This is the readpool only, for write pool we do not count, because usually it's only 1
	// fmt.Println("Status == ", CurrentNode.Status)
	// fmt.Println("Status.MaxPool == ", CurrentNode.Status.MaxPool)
	// fmt.Println("Status.Peers == ", len(CurrentNode.Status.Peers))
	if len(CurrentNode.Status.Peers) > 0 {
		CurrentNode.MaxPool = CurrentNode.Status.MaxPool * len(CurrentNode.Status.Peers)
	}
	return nil
}

// This is the status for SureSQL Nodes (not the internal DBMS nodes)
// Status is pretty much taken from Settings, but this is used for response
func (n *SureSQLNode) GetStatusFromSettings(conf SureSQLDBMSConfig) {
	// CurrentNode.Status.StatusStruct.SettingsTable = CurrentNode.Settings
	if n.Status.Peers == nil {
		n.Status.Peers = make(map[int]orm.StatusStruct)
	}
	n.Status.Version = APP_VERSION
	// if NodeNumber == 1 then it is Leader
	n.Status.IsLeader = n.Config.NodeNumber == LEADER_NODE_NUMBER
	n.Status.URL = "http://"
	if n.Config.SSL {
		n.Status.URL = "https://"
	}
	n.Status.URL += n.Config.Host
	if n.Config.Port != "" {
		CurrentNode.Status.URL += ":" + CurrentNode.Config.Port
	}
	n.Status.StartTime = ServerStartTime
	n.Status.Uptime = time.Since(ServerStartTime) // this is refreshed when Status handler is called
	n.Status.Mode = n.Config.Mode
	n.Status.Nodes = n.Config.Nodes
	n.Status.NodeNumber = n.Config.NodeNumber
	n.Status.NodeID = fmt.Sprintf("%d", n.Status.NodeNumber)
	n.Status.DBMS = n.Config.DBMS
	// These are filled during getStatusInternal
	// LastBackup
	// Leader
	// NodeID
	// DirSize
	// DBSize
}

// Apply config if they are changed from DB, only few that can be changed and effected at run-time
// NOTE: this is hard-coded
func (n *SureSQLNode) ApplySettings(category, key string) bool {
	res := false
	tmp, ok := n.Settings.SettingExist(category, key)

	switch category {
	case SETTING_CATEGORY_TOKEN:
		switch key {
		case SETTING_KEY_TOKEN_EXP:
			if !ok {
				n.Config.TokenExp = DEFAULT_TOKEN_EXPIRES_MINUTES
			} else {
				n.Config.TokenExp = time.Duration(tmp.IntValue) * time.Minute
			}
			res = true
		case SETTING_KEY_REFRESH_EXP:
			if !ok {
				n.Config.RefreshExp = DEFAULT_REFRESH_EXPIRES_MINUTES
			} else {
				n.Config.RefreshExp = time.Duration(tmp.IntValue) * time.Minute
			}
			res = true
		case SETTING_KEY_TOKEN_TTL:
			if !ok {
				n.Config.TTLTicker = DEFAULT_TTL_TICKER_MINUTES
			} else {
				n.Config.TTLTicker = time.Duration(tmp.IntValue) * time.Minute
			}
			res = true
		default:
		}
	case SETTING_CATEGORY_CONNECTION:
		switch key {
		case SETTING_KEY_ENABLE_POOL:
			if ok {
				n.IsPoolEnabled = tmp.IntValue == 1
				res = true
			} else {
				n.IsPoolEnabled = DEFAULT_POOL_ENABLED
			}
		case SETTING_KEY_MAX_POOL:
			if ok {
				n.MaxPool = tmp.IntValue
				if n.MaxPool == 0 && n.IsPoolEnabled {
					n.MaxPool = DEFAULT_MAX_POOL
				}
				res = true
			} else {
				n.MaxPool = DEFAULT_MAX_POOL
			}
		default:
		}
	case SETTING_CATEGORY_NODES:
		nodes := len(n.Settings[SETTING_CATEGORY_NODES])
		for _, c := range n.Settings[SETTING_CATEGORY_NODES] {
			// value string: node_number;hostname;ip;mode
			// -- node_number|hostname|ip|mode   and the CONFIG_NODE_DELIMITER in this case is "|"

			parsed := strings.Split(c.TextValue, SETTING_NODE_DELIMITER)
			stat := orm.StatusStruct{
				// overwrite the actual DBMS node_ID to use SureSQL NodeNumber as the string-type ID
				NodeID:     parsed[0],
				NodeNumber: object.Int(parsed[0], false), // this current node number
				URL:        parsed[1],
				Nodes:      nodes, // number of nodes
				Mode:       parsed[3],
				MaxPool:    n.Status.MaxPool,
			}
			// Because the config contains the whole cluster information, including the master/this current node
			// If not the same NodeNumber then it's the peers.
			if n.Config.NodeNumber != stat.NodeNumber {
				n.Status.Peers[stat.NodeNumber] = stat
			}
		}
	case SETTING_CATEGORY_EMPTY:
	default:
	}
	return res
}

// This is to get all the config table and put it as SureSQLNode config
func (n *SureSQLNode) ApplyAllConfig() bool {
	if n.Status.Peers == nil {
		n.Status.Peers = make(map[int]orm.StatusStruct)
	}
	res := true
	res = n.ApplySettings(SETTING_CATEGORY_CONNECTION, SETTING_KEY_MAX_POOL)
	res = n.ApplySettings(SETTING_CATEGORY_CONNECTION, SETTING_KEY_ENABLE_POOL) || res
	res = n.ApplySettings(SETTING_CATEGORY_TOKEN, SETTING_KEY_TOKEN_EXP) || res
	res = n.ApplySettings(SETTING_CATEGORY_TOKEN, SETTING_KEY_REFRESH_EXP) || res
	res = n.ApplySettings(SETTING_CATEGORY_TOKEN, SETTING_KEY_TOKEN_TTL) || res
	res = n.ApplySettings(SETTING_CATEGORY_NODES, "no need key") || res
	return res
}

func GetStatusInternal(db SureSQLDB, setNodeStatus bool) (orm.NodeStatusStruct, error) {
	status, err := db.Status()
	if err != nil {
		return orm.NodeStatusStruct{}, err
	}
	if setNodeStatus {
		CurrentNode.Status.DirSize = status.DirSize
		CurrentNode.Status.DBSize = status.DBSize
		// CurrentNodeID is not the DBMS NodeID. status.NodeID is the DBMS NodeID (if clustered)
		// CurrentNode.Status.NodeID = status.NodeID
		CurrentNode.Status.LastBackup = status.LastBackup
		CurrentNode.Status.Leader = status.Leader
		if CurrentNode.Status.MaxPool == 0 {
			if CurrentNode.MaxPool != 0 {
				CurrentNode.Status.MaxPool = CurrentNode.MaxPool
			} else {
				CurrentNode.Status.MaxPool = DEFAULT_MAX_POOL
			}
		}
		CurrentNode.Status.Uptime = time.Since(ServerStartTime) // this is refreshed when Status handler is called

	}
	return status, err
}

// Print the node information for console log
func (n SureSQLNode) PrintWelcomePretty() {
	fmt.Printf("")
	if n.InternalConnection == nil {
		fmt.Println("Database not connected - nil")
		return
	} else if !n.InternalConnection.IsConnected() {
		fmt.Println("Database not connected - function")
		return
	}

	prot := "http://"
	heading1 := APP_NAME + " " + APP_VERSION
	heading2 := fmt.Sprintf("%s (%d) - Node %d/%d", n.Config.Label, n.Config.NodeID, n.Config.NodeNumber, n.Config.Nodes)
	if n.Config.SSL {
		prot = "https://"
	}
	heading3 := fmt.Sprintf("%s%s:%s", prot, n.Config.Host, n.Config.Port)
	appName := []string{heading1, heading2, heading3}
	headingColors := []print.Color{
		print.ColorCyan,
		print.ColorGreen,
		print.ColorNothing,
	}

	hardtoken := false
	if n.InternalConfig.Token != "" {
		hardtoken = true
	}
	hardjwe := false
	if n.InternalConfig.JWEKey != "" {
		hardjwe = true
	}
	consistency := n.InternalConfig.Consistency
	if consistency == "" {
		consistency = "default"
	}
	apikey := false
	if n.Config.APIKey != "" {
		apikey = true
	}
	clientid := false
	if n.Config.ClientID != "" {
		clientid = true
	}

	var clusters []print.KeyValue
	leader, err := n.InternalConnection.Leader()

	if err == nil {
		clusters = append(clusters, print.Content(true, false, "Leader", leader))
	}
	peers, err := n.InternalConnection.Peers()
	if err == nil {
		if len(peers) > 1 {
			for i, p := range peers {
				pstr := fmt.Sprintf("Peer %d", i)
				clusters = append(clusters, print.Content(true, false, pstr, p))
			}
		} else {
			clusters = append(clusters, print.Content(true, false, "Peers", "None/Single Node"))
		}
	}

	// add a new line between leaders/peers information and settings
	clusters = append(clusters, print.Content(true, true, "", ""))

	// Content defined in order
	appSettings := []print.KeyValue{
		print.Content(false, false, "Mode", n.Config.Mode),
		print.Content(false, false, "Split-write", n.Config.IsSplitWrite),
		print.Content(false, false, "IP", n.Config.IP),
		print.Content(false, false, "DB init", n.Config.IsInitDone),
		print.Content(false, false, "Pool", n.IsPoolEnabled),
		print.Content(false, false, "Max pools", n.MaxPool),
		print.Content(false, false, "Encryption", n.Config.EncryptionMethod),
		print.Content(false, false, "Hard token", hardtoken),
		print.Content(false, false, "Hard JWE", hardjwe),
		print.Content(false, false, "API key", apikey),
		print.Content(false, false, "Client ID", clientid),
		print.Content(false, false, "Consistency", consistency),
		print.Content(true, false, "Options", n.InternalConfig.Options),
	}

	keyColor := print.ColorNothing
	valueColor := print.ColorLightBlue
	appSettings = append(clusters, appSettings...)

	print.PrintBoxHeadingContent(appName, headingColors, appSettings, keyColor, valueColor)
}
