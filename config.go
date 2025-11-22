package suresql

import (
	"fmt"
	"strings"
	"sync"

	orm "github.com/medatechnology/simpleorm"

	utils "github.com/medatechnology/goutil"
	"github.com/medatechnology/goutil/medaerror"
	"github.com/medatechnology/goutil/object"
)

const (
	// DEFAULT LEADER NODE
	LEADER_NODE_NUMBER = 1

	// for readibility
	INTERNAL_MODE = false // not copying the result into current node's status
	NODE_MODE     = true  // copying the result into current node's status

	// ConfigTable Categories and keys
	SETTING_CATEGORY_TOKEN  = "token"
	SETTING_KEY_TOKEN_EXP   = "token_exp"   // value int: in minutes
	SETTING_KEY_REFRESH_EXP = "refresh_exp" // value int: in minutes
	SETTING_KEY_TOKEN_TTL   = "token_ttl"   // value int: in minutes, beat for checking expiration

	SETTING_CATEGORY_CONNECTION = "connection"
	SETTING_KEY_MAX_POOL        = "max_pool" // value int: 0 overwrite pool_on, meaning no pooling, automatically pool_on=false
	SETTING_KEY_ENABLE_POOL     = "pool_on"  // value string: true or false

	SETTING_CATEGORY_NODES = "nodes"
	SETTING_KEY_NODE_NAME  = "node_name" // value string: node_number;hostname;ip;mode
	SETTING_NODE_DELIMITER = "|"

	SETTING_CATEGORY_SYSTEM       = "system"
	SETTING_KEY_LABEL             = "label"             // value string: label for this node
	SETTING_KEY_IP                = "ip"                // value string: database name
	SETTING_KEY_HOST              = "host"              // value string: hostname
	SETTING_KEY_PORT              = "port"              // value string: port number
	SETTING_KEY_SSL               = "ssl"               // value bool(int): true or false
	SETTING_KEY_DBMS              = "dbms"              // value string: rqlite or other later implementation
	SETTING_KEY_MODE              = "mode"              // value string: 'r', 'w', 'rw'
	SETTING_KEY_NODES             = "nodes"             // value int: total nodes in the cluster
	SETTING_KEY_NODE_NUMBER       = "node_number"       // value int: node number for this server
	SETTING_KEY_IS_INIT_DONE      = "is_init_done"      // value bool(int): DB init is done
	SETTING_KEY_IS_SPLIT_WRITE    = "is_split_write"    // value bool(int): split write
	SETTING_KEY_ENCRYPTION_METHOD = "encryption_method" // value string: "aes", "rsa", "none"

	SETTING_CATEGORY_EMPTY = "nocategory"
)

// map SettingTable by the key (string) which is same as SettingTable.SettingKey
// instead of using array, this is faster to search for specific setting key
type SettingsMap map[string]SettingTable

// Map SettingsMap by the category, which is the same as inside SettingTable.Category
// Finding key based on category: Settings[category][Key] ie: Settings[token][token_exp].IntValue =
type Settings map[string]SettingsMap

// This is config needed by SureSQL to connect to Internal DB (DBMS), at this point only RQLite
type SureSQLDBMSConfig struct {
	Host        string `json:"host,omitempty"            db:"host"`
	Port        string `json:"port,omitempty"            db:"port"`
	Username    string `json:"username,omitempty"        db:"username"` // this is not used, we use _users table instead
	Password    string `json:"password,omitempty"        db:"password"` // this is not used, we use _users table instead
	Database    string `json:"database,omitempty"        db:"database"`
	SSL         bool   `json:"ssl,omitempty"             db:"ssl"`
	Options     string `json:"options,omitempty"         db:"options"`
	Consistency string `json:"consistency,omitempty"     db:"consistency"`
	// below are not yet used. Previously those are SureSQL Config instead of DBMS config
	URL string `json:"url,omitempty"             db:"url"`
	EnvConfig
	// Token        string        `json:"token,omitempty"           db:"token"`
	// RefreshToken string        `json:"refresh_token,omitempty"   db:"refresh_token"`
	// JWEKey       string        `json:"jwe_key,omitempty"         db:"jwe_key"`
	// APIKey       string        `json:"api_key,omitempty"         db:"api_key"`
	// ClientID     string        `json:"client_id,omitempty"       db:"client_id"`
	// HttpTimeout  time.Duration `json:"http_timeout,omitempty"    db:"http_timeout"`
	// RetryTimeout time.Duration `json:"retry_timeout,omitempty"   db:"retry_timeout"`
	// MaxRetries   int           `json:"max_retries,omitempty"     db:"max_retries"`
}

func (sc *SureSQLDBMSConfig) PrintDebug(secure bool) {
	fmt.Println("Loading from environment")
	fmt.Println("Host          : ", sc.Host)
	fmt.Println("Port          : ", sc.Port)
	fmt.Println("UserName      : ", sc.Username)
	fmt.Println("Database      : ", sc.Database)
	fmt.Println("SSL           : ", sc.SSL)
	fmt.Println("Options       : ", sc.Options)
	fmt.Println("URL           : ", sc.URL)
	fmt.Println("HTTP Timeout  : ", sc.URL)
	fmt.Println("Retry Timeout : ", sc.URL)
	fmt.Println("Max Retries   : ", sc.URL)
	if secure {
		fmt.Println("Password      : ", sc.Password)
		fmt.Println("Token         : ", sc.Token)
		fmt.Println("Refresh       : ", sc.RefreshToken)
		fmt.Println("JWEKey        : ", sc.JWEKey)
		fmt.Println("APIKey        : ", sc.APIKey)
		fmt.Println("ClientID      : ", sc.ClientID)
	}
}

// If using direct-rqlite (our own) implementation, then no need, because when direct-rqlite connects to
// RQLite server it will use basic-auth format for the username and password.
func (sc *SureSQLDBMSConfig) GenerateRQLiteURL() {
	tmpURL := "http://"
	if sc.SSL {
		tmpURL = "https://"
	}
	if len(sc.Host) > 0 {
		tmpURL += sc.Host
	} else {
		tmpURL += "localhost"
		fmt.Println("ERROR! No Host defined in environment")
	}
	if len(sc.Port) > 0 {
		tmpURL += ":" + sc.Port
	}
	sc.URL = tmpURL
}

// NOTE: this is not used, because we are using direct-rqlite implementation
// If using the gorqlite implementation, then we need to put username+password in the URL
// then gorqlite use this to connect to the rqlite server
func (sc *SureSQLDBMSConfig) GenerateGoRQLiteURL() {
	tmpURL := "http://"
	if sc.SSL {
		tmpURL = "https://"
	}
	if len(sc.Username) > 0 {
		tmpURL += sc.Username
	}
	if len(sc.Password) > 0 {
		tmpURL += ":" + sc.Password
	}
	if len(sc.Username) > 0 || len(sc.Password) > 0 {
		tmpURL += "@"
	}
	if len(sc.Host) > 0 {
		tmpURL += sc.Host
	} else {
		fmt.Println("ERROR! No Host defined in environment")
	}
	if len(sc.Port) > 0 {
		tmpURL += ":" + sc.Port
	}
	tmpURL += "/"
	if len(sc.Options) > 0 {
		tmpURL += "?" + sc.Options
	}
	sc.URL = tmpURL
}

// Cache for DBMS configuration to avoid repeated environment variable lookups
var (
	cachedDBMSConfig SureSQLDBMSConfig
	dbmsConfigOnce   sync.Once
)

// Reading internal DB configuration for this SureSQL Node, from environment
// Cached after first load for performance. Use ReloadDBMSConfig() to force reload.
func LoadDBMSConfigFromEnvironment() SureSQLDBMSConfig {
	dbmsConfigOnce.Do(func() {
		cachedDBMSConfig = loadDBMSConfigFromEnvironment()
	})
	return cachedDBMSConfig
}

// Internal function that actually loads from environment (not cached)
func loadDBMSConfigFromEnvironment() SureSQLDBMSConfig {
	tmpConfig := SureSQLDBMSConfig{
		Host:        utils.GetEnvString("DBMS_HOST", ""),
		Port:        utils.GetEnvString("DBMS_PORT", ""),
		Username:    utils.GetEnvString("DBMS_USERNAME", ""),
		Password:    utils.GetEnvString("DBMS_PASSWORD", ""),
		Database:    utils.GetEnvString("DBMS_DATABASE", ""),
		SSL:         utils.GetEnvBool("DBMS_SSL", false),
		Options:     utils.GetEnvString("DBMS_OPTIONS", ""),
		Consistency: utils.GetEnvString("DBMS_CONSISTENCY", ""),
		EnvConfig: EnvConfig{
			Token:        utils.GetEnvString("DBMS_TOKEN", ""),
			RefreshToken: utils.GetEnvString("DBMS_TOKEN_REFRESH", ""),
			JWEKey:       utils.GetEnvString("DBMS_JWE_KEY", ""),
			JWTKey:       utils.GetEnvString("DBMS_JWT_KEY", ""),
			APIKey:       utils.GetEnvString("DBMS_API_KEY", ""),
			ClientID:     utils.GetEnvString("DBMS_CLIENT_ID", ""),
			HttpTimeout:  utils.GetEnvDuration("DBMS_HTTP_TIMEOUT", DEFAULT_TIMEOUT),
			RetryTimeout: utils.GetEnvDuration("DBMS_RETRY_TIMEOUT", DEFAULT_RETRY_TIMEOUT),
			MaxRetries:   utils.GetEnvInt("DBMS_MAX_RETRIES", DEFAULT_RETRY),
		},
	}
	return tmpConfig
}

// ReloadDBMSConfig forces a reload of DBMS configuration from environment
// Useful if environment variables change at runtime
func ReloadDBMSConfig() SureSQLDBMSConfig {
	cachedDBMSConfig = loadDBMSConfigFromEnvironment()
	return cachedDBMSConfig
}

// if DB settings is not there, get from environment. DB's settings table always wins
func OverwriteConfigFromEnvironment() {
	ip := utils.GetEnvString("SURESQL_IP", "")
	if ip != "" {
		CurrentNode.Config.IP = ip
	}
	host := utils.GetEnvString("SURESQL_HOST", "")
	if host != "" {
		CurrentNode.Config.Host = host
	}
	port := utils.GetEnvString("SURESQL_PORT", "")
	if port != "" {
		CurrentNode.Config.Port = port
	}
	dbms := utils.GetEnvString("SURESQL_DBMS", "")
	if dbms != "" {
		CurrentNode.Config.DBMS = dbms
	}
	iAPI := utils.GetEnvString("SURESQL_INTERNAL_API", "")
	if iAPI != "" {
		CurrentNode.InternalAPI = iAPI
		// Parse internal API credentials (format: username:password)
		if len(iAPI) > 0 {
			parts := strings.Split(iAPI, ":")
			if len(parts) >= 2 {
				CurrentNode.InternalConfig.Username = parts[0]
				CurrentNode.InternalConfig.Password = parts[1]
			}
		}
	}
	apiKey := utils.GetEnvString("SURESQL_API_KEY", "")
	if apiKey != "" {
		CurrentNode.Config.APIKey = apiKey
	}
	clientID := utils.GetEnvString("SURESQL_CLIENT_ID", "")
	if clientID != "" {
		CurrentNode.Config.ClientID = clientID
	}
	token := utils.GetEnvString("SURESQL_TOKEN", "")
	if token != "" {
		CurrentNode.Config.Token = token
	}
	refreshToken := utils.GetEnvString("SURESQL_REFRESH_TOKEN", "")
	if refreshToken != "" {
		CurrentNode.Config.RefreshToken = refreshToken
	}
	jweKey := utils.GetEnvString("SURESQL_JWE_KEY", "")
	if jweKey != "" {
		CurrentNode.Config.JWEKey = jweKey
	}
	jwtKey := utils.GetEnvString("SURESQL_JWT_KEY", "")
	if jwtKey != "" {
		CurrentNode.Config.JWTKey = jwtKey
	}
	timeout := utils.GetEnvDuration("SURESQL_HTTP_TIMEOUT", DEFAULT_TIMEOUT)
	if timeout > 0 {
		CurrentNode.Config.HttpTimeout = timeout
	}
	retryTimeout := utils.GetEnvDuration("SURESQL_RETRY_TIMEOUT", DEFAULT_RETRY_TIMEOUT)
	if retryTimeout > 0 {
		CurrentNode.Config.RetryTimeout = retryTimeout
	}
	maxRetries := utils.GetEnvInt("SURESQL_MAX_RETRIES", DEFAULT_RETRY)
	if maxRetries > 0 {
		CurrentNode.Config.MaxRetries = maxRetries
	}
	tokenExp := utils.GetEnvDuration("SURESQL_TOKEN_EXP", 0)
	if tokenExp > 0 {
		CurrentNode.Config.TokenExp = tokenExp
	}
	refreshExp := utils.GetEnvDuration("SURESQL_REFRESH_EXP", 0)
	if refreshExp > 0 {
		CurrentNode.Config.RefreshExp = refreshExp
	}
	tokenTTL := utils.GetEnvDuration("SURESQL_TOKEN_TTL", 0)
	if tokenTTL > 0 {
		CurrentNode.Config.TTLTicker = tokenTTL
	}
}

// LoadConfigFromDB loads settings from _settings table
func LoadConfigFromDB(db *SureSQLDB) error {
	record, err := (*db).SelectOne(CurrentNode.Config.TableName())
	if err != nil {
		return medaerror.Errorf("failed to load settings: %v", err)
	}

	// Get from database
	CurrentNode.Config = object.MapToStructSlow[ConfigTable](record.Data)
	CurrentNode.IsEncrypted = CurrentNode.Config.EncryptionMethod != "none"
	OverwriteConfigFromEnvironment()
	// TODO (Clustering): Fetch cluster peers and leader information from DBMS status endpoint
	// Currently status comes from local config only. Should query actual cluster state.
	// Priority: Medium | Needed for: Multi-node deployments
	return nil
}

func LoadSettingsFromDB(db *SureSQLDB) error {
	records, err := (*db).SelectMany(SettingTable{}.TableName())
	if err != nil {
		if err != orm.ErrSQLNoRows {
			return nil
		}
		return medaerror.Errorf("failed to load configs from DB: %v", err)
	}
	for _, r := range records {
		tmp := object.MapToStruct[SettingTable](r.Data)
		if tmp.Category == "" {
			tmp.Category = SETTING_CATEGORY_EMPTY
		}
		tmpConfigMap, ok := CurrentNode.Settings[tmp.Category]
		if !ok {
			tmpConfigMap = make(SettingsMap)
		}
		tmpConfigMap[tmp.SettingKey] = tmp
		CurrentNode.Settings[tmp.Category] = tmpConfigMap
	}
	// fmt.Println("DEBUG: reading configs table:", len(records), " rows")
	// fmt.Println("DEBUG: current node configs :", len(CurrentNode.DBConfigs), " category")
	return err
}

// By category and key
func (c Settings) SettingExist(category, key string) (SettingTable, bool) {
	if category == "" {
		category = SETTING_CATEGORY_EMPTY
	}
	if tmp, ok := c[category]; ok {
		if conf, ok := tmp.SettingExist(key); ok {
			return conf, true
		}
	}
	return SettingTable{}, false
}

func (c SettingsMap) SettingExist(key string) (SettingTable, bool) {
	if conf, ok := c[key]; ok {
		return conf, true
	}
	return SettingTable{}, false
}
