package suresql

import (
	"fmt"
	"strings"
	"time"

	"github.com/medatechnology/simpleorm/postgres"
	"github.com/medatechnology/simpleorm/rqlite"
)

var (
	ServerStartTime time.Time
)

// Making connection to internal DB
// This is where implementation selection happens based on DBMS configuration
func NewDatabase(conf SureSQLDBMSConfig) (SureSQLDB, error) {
	// Determine which database driver to use based on DBMS configuration
	// Default to RQLite if not specified
	dbmsType := strings.ToUpper(strings.TrimSpace(conf.DBMS))
	if dbmsType == "" {
		dbmsType = "RQLITE"
	}

	switch dbmsType {
	case "POSTGRESQL", "POSTGRES":
		return newPostgreSQLDatabase(conf)
	case "RQLITE":
		return newRQLiteDatabase(conf)
	default:
		return nil, fmt.Errorf("unsupported DBMS type: %s (supported: RQLITE, POSTGRESQL)", conf.DBMS)
	}
}

// newPostgreSQLDatabase creates a new PostgreSQL database connection
func newPostgreSQLDatabase(conf SureSQLDBMSConfig) (SureSQLDB, error) {
	port := 5432
	if conf.Port != "" {
		fmt.Sscanf(conf.Port, "%d", &port)
	}

	config := postgres.PostgresConfig{
		Host:     conf.Host,
		Port:     port,
		User:     conf.Username,
		Password: conf.Password,
		DBName:   conf.Database,
		SSLMode:  "disable",
	}

	if conf.SSL {
		config.SSLMode = "require"
	}

	// PostgreSQL uses standard schema tables
	SchemaTable = "information_schema.tables"
	CurrentNode.Status.DBMSDriver = "postgres"
	return postgres.NewDatabase(config)
}

// newRQLiteDatabase creates a new RQLite database connection
func newRQLiteDatabase(conf SureSQLDBMSConfig) (SureSQLDB, error) {
	conf.GenerateRQLiteURL()

	config := rqlite.RqliteDirectConfig{
		URL:         conf.URL,
		Consistency: conf.Consistency,
		Username:    conf.Username,
		Password:    conf.Password,
		Timeout:     conf.HttpTimeout,
		RetryCount:  conf.MaxRetries,
	}
	SchemaTable = rqlite.SCHEMA_TABLE
	CurrentNode.Status.DBMSDriver = "direct-rqlite"
	return rqlite.NewDatabase(config)
}
