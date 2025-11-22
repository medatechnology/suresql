package suresql

import (
	"sync"
	"time"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/goutil/medaerror"
	"github.com/medatechnology/goutil/medattlmap"
)

const (
	// Default Token settings
	DEFAULT_TOKEN_EXPIRES_MINUTES   = 24 * 60 * time.Minute // every 24 hours
	DEFAULT_REFRESH_EXPIRES_MINUTES = 48 * 60 * time.Minute // every 48 hours
	DEFAULT_TTL_TICKER_MINUTES      = 5 * time.Minute       // every [value] minute, check for expiration for ttl

	// Default HTTP timeouts
	// DEFAULT_CONNECTION_TIMEOUT            = 60 * time.Second
	DEFAULT_TIMEOUT       = 60 * time.Second
	DEFAULT_RETRY_TIMEOUT = 60 * time.Second
	DEFAULT_RETRY         = 3

	// Default Pool settings
	DEFAULT_MAX_POOL     = 25
	DEFAULT_POOL_ENABLED = true
)

// GLOBAL VAR
var (
	CurrentNode       SureSQLNode
	ReloadEnvironment bool = false

	// Standard errors using medaerror for consistency
	ErrNoDBConnection       = medaerror.MedaError{Message: "no db connection"}
	ErrDBInitializedAlready = medaerror.MedaError{Message: "DB already initialized"}
	SchemaTable string = ""
	// EmptyConnection SureSQLDB = SureSQLDB{}
)

type SureSQLDB orm.Database

// StandardResponse is a structured response format for all API responses
type StandardResponse struct {
	Status  int         `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// ===== Used in handle_SQL endpoints
// SQLRequest represents the request structure for executing SQL commands: UPDATE, DELETE, DROP, INSERT, SELECT
type SQLRequest struct {
	Statements []string                `json:"statements,omitempty"` // Raw SQL statements to execute
	ParamSQL   []orm.ParametereizedSQL `json:"param_sql,omitempty"`  // Parameterized SQL statements to execute
	SingleRow  bool                    `json:"single_row,omitempty"` // If true, return only first row
}

// SQLResponse represents the response structure for SQL execution results
type SQLResponse struct {
	Results       []orm.BasicSQLResult `json:"results"`        // Results for each executed statement
	ExecutionTime float64              `json:"execution_time"` // Total execution time in milliseconds
	RowsAffected  int                  `json:"rows_affected"`  // Total number of rows affected
}

// ===== Used in handle_Query endpoints
// QueryRequest represents the simplified request structure for executing SELECT queries
type QueryRequest struct {
	Table     string         `json:"table"`                // Table name for queries
	Condition *orm.Condition `json:"condition,omitempty"`  // Optional condition for filtering
	SingleRow bool           `json:"single_row,omitempty"` // If true, return only first row
}

// QueryResponse represents the response structure for query results
type QueryResponse struct {
	Records       []orm.DBRecord `json:"records"` // Always returns as array, even for single record
	ExecutionTime float64        `json:"execution_time"`
	Count         int            `json:"count"`
}

// QueryRequest represents the simplified request structure for executing SELECT queries

// QueryResponse represents the response structure for query results
type QueryResponseSQL []QueryResponse

// ===== Used in handle_Insert endpoints
// InsertRequest represents the request structure for inserting records
type InsertRequest struct {
	Records   []orm.DBRecord `json:"records"`              // Records to insert
	Queue     bool           `json:"queue,omitempty"`      // Whether to use queue operations (optional)
	SameTable bool           `json:"same_table,omitempty"` // Indicates if all records belong to the same table
}

// Originally this was saved in DB as table, but maybe Redis or some auto-expire system is better
type TokenTable struct {
	ID               string    `json:"id,omitempty"                  db:"id"`
	UserID           string    `json:"user_id,omitempty"             db:"user_id"`
	Token            string    `json:"token,omitempty"               db:"token"`
	Refresh          string    `json:"refresh_token,omitempty"       db:"refresh_token"`
	TokenExpiresAt   time.Time `json:"token_expired_at,omitempty"    db:"token_expired_at"`
	RefreshExpiresAt time.Time `json:"refresh_expired_at,omitempty"  db:"refresh_expired_at"`
	CreatedAt        time.Time `json:"created_at,omitempty"          db:"created_at"`
	// additional members
	UserName string
}

func (t TokenTable) TableName() string {
	return "_tokens"
}

// This is reserved to be configuration that usually taken from environment variables for safety
type EnvConfig struct {
	Token        string        `json:"token,omitempty"           db:"token"`
	RefreshToken string        `json:"refresh_token,omitempty"   db:"refresh_token"`
	JWEKey       string        `json:"jwe_key,omitempty"         db:"jwe_key"`
	JWTKey       string        `json:"jwt_key,omitempty"         db:"jwt_key"`
	APIKey       string        `json:"api_key,omitempty"         db:"api_key"`
	ClientID     string        `json:"client_id,omitempty"       db:"client_id"`
	HttpTimeout  time.Duration `json:"http_timeout,omitempty"    db:"http_timeout"`
	RetryTimeout time.Duration `json:"retry_timeout,omitempty"   db:"retry_timeout"`
	MaxRetries   int           `json:"max_retries,omitempty"     db:"max_retries"`
}

// This is the config for the current SureSQL node, it inserted inside the table!
type ConfigTable struct {
	ID               int           `json:"id,omitempty"                  db:"id"`
	Label            string        `json:"label,omitempty"               db:"label"`
	IP               string        `json:"ip,omitempty"                  db:"ip"`
	Host             string        `json:"host,omitempty"                db:"host"`
	Port             string        `json:"port,omitempty"                db:"port"`
	SSL              bool          `json:"ssl,omitempty"                 db:"ssl"`
	DBMS             string        `json:"dbms,omitempty"                db:"dbms"`
	Mode             string        `json:"mode,omitempty"                db:"mode"`
	Nodes            int           `json:"nodes,omitempty"               db:"nodes"`       // total number of nodes in the cluster
	NodeNumber       int           `json:"node_number,omitempty"         db:"node_number"` // this is node number .. X
	NodeID           int           `json:"node_id,omitempty"             db:"node_id"`     // this is node ID from rqlite cluster
	IsInitDone       bool          `json:"is_init_done,omitempty"        db:"is_init_done"`
	IsSplitWrite     bool          `json:"is_split_write,omitempty"      db:"is_split_write"`
	EncryptionMethod string        `json:"encryption_method,omitempty"   db:"encryption_method"`
	TokenExp         time.Duration `json:"token_exp,omitempty"           db:"token_exp"`   // token expiration in minutes
	RefreshExp       time.Duration `json:"refresh_exp,omitempty"         db:"refresh_exp"` // refresh token expiration in minutes
	TTLTicker        time.Duration `json:"ttl_ticker,omitempty"          db:"ttl_ticker"`  // ttl ticker to check expiration in minutes
	EnvConfig
}

func (s ConfigTable) TableName() string {
	return "_configs"
}

// This is how we store the config for SureSQL. It can contains the peers information, timeouts etc (depends on the category)
// Ie: category: token , rows are:
// ConfigKey: token_exp , IntValue: 20 (in minutes)
// ConfigKey: refresh_exp , IntValue: 200 (in minutes)
// ConfigKey: token_ttl , IntValue: 5 (in minutes)
type SettingTable struct {
	ID         int     `json:"id,omitempty"                  db:"id"`
	Category   string  `json:"category,omitempty"            db:"category"`
	DataType   string  `json:"data_type,omitempty"           db:"data_type"`
	SettingKey string  `json:"setting_key,omitempty"         db:"setting_key"`
	TextValue  string  `json:"text_value,omitempty"          db:"text_value"`
	FloatValue float64 `json:"float_value,omitempty"         db:"float_value"`
	IntValue   int     `json:"int_value,omitempty"           db:"int_value"`
}

func (c SettingTable) TableName() string {
	return "_settings"
}

// GetValue returns the value of the config entry as an interface{} based on data_type
func (c SettingTable) GetValue() interface{} {
	switch c.DataType {
	case "text", "string":
		return c.TextValue
	case "float", "double":
		return c.FloatValue
	case "int", "integer":
		return c.IntValue
	case "bool", "boolean":
		// Convert stored text value to boolean
		if c.TextValue == "true" || c.TextValue == "1" || c.TextValue == "yes" ||
			c.IntValue == 1 {
			return true
		}
		return false
	default:
		// Default to text value
		return c.TextValue
	}
}

// Status for the node, contains the peers if applicable, mostly from SettingsTable but used for response.
// NOTE: This type is not used - we use orm.NodeStatusStruct instead
// type NodeStatusStruct struct {
// 	Status StatusStruct
// 	Peers  []StatusStruct // all peers including the leader
// }

// This is the whole SureSQL Node is all about
// NOTE: do we need IP? because we can put IP address in the hostname field if we
// are connecting based on IP.
type SureSQLNode struct {
	mu                 sync.RWMutex         // Protects concurrent access to node state
	InternalConfig     SureSQLDBMSConfig    `json:"internal_config,omitempty"      db:"internal_config"`
	InternalAPI        string               `json:"internal_api,omitempty"         db:"internal_api"`        // This is for the node internal API (CRUD users)
	Config             ConfigTable          `json:"settings,omitempty"             db:"settings"`            // Settings for this node, from DB table
	Settings           Settings             `json:"configs,omitempty"              db:"configs"`             // Configs for this node, from DB table
	Status             orm.NodeStatusStruct `json:"status,omitempty"               db:"status"`              // Status for SureSQL DB Node that is standard from orm
	InternalConnection SureSQLDB            `json:"internal_connection,omitempty"  db:"internal_connection"` // master connection to InternalDB
	DBConnections      *medattlmap.TTLMap   `json:"db_connections,omitempty"       db:"db_connections"`      // another connection based on Token
	MaxPool            int                  `json:"max_pool,omitempty"             db:"max_pool"`            // total nodes for this project
	IsPoolEnabled      bool                 `json:"is_poolenabled,omitempty"       db:"is_poolenabled"`      // if this DB already initialized
	IsEncrypted        bool                 `json:"is_encrypted,omitempty"         db:"is_encrypted"`        // none/AES/Bcrypt (already in Settings)
	// IP                 string               `json:"ip,omitempty"                   db:"ip"`                  // IP for this sureSQL node
	// TokenExp           time.Duration        `json:"token_exp,omitempty"            db:"token_exp"`           // token expiration in minutes
	// RefreshExp         time.Duration        `json:"refresh_exp,omitempty"          db:"refresh_exp"`         // refresh token expiration in minutes
	// TTLTicker          time.Duration        `json:"ttl_ticker,omitempty"           db:"ttl_ticker"`          // ttl ticker to check expiration in minutes
}
