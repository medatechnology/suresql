package main

import (
	"fmt"

	orm "github.com/medatechnology/simpleorm"
	"github.com/medatechnology/suresql"

	utils "github.com/medatechnology/goutil"
	"github.com/medatechnology/goutil/simplelog"
)

func main() {
	simplelog.DEBUG_LEVEL = 1
	simplelog.LogThis("=== SureSQL PostgreSQL Example ===")
	simplelog.LogThis("This example demonstrates using SureSQL abstraction with PostgreSQL backend")

	// Load environment variables
	utils.ReloadEnvEach(".env")

	// Configure database connection using SureSQL abstraction
	// This will automatically select PostgreSQL driver based on DBMS_TYPE
	config := suresql.SureSQLDBMSConfig{
		DBMS:     utils.GetEnv("DBMS_TYPE", "POSTGRESQL"),
		Host:     utils.GetEnv("DBMS_HOST", "localhost"),
		Port:     utils.GetEnv("DBMS_PORT", "5432"),
		Username: utils.GetEnv("DBMS_USERNAME", "postgres"),
		Password: utils.GetEnv("DBMS_PASSWORD", "password"),
		Database: utils.GetEnv("DBMS_DATABASE", "testdb"),
		SSL:      utils.GetEnvBool("DBMS_SSL", false),
	}

	simplelog.LogThis("Connecting to database via SureSQL abstraction...")
	simplelog.LogThis(fmt.Sprintf("Config: DBMS=%s, Host=%s, Port=%s, User=%s, Database=%s",
		config.DBMS, config.Host, config.Port, config.Username, config.Database))

	// Create database connection using SureSQL abstraction
	// This automatically selects the right driver (PostgreSQL in this case)
	db, err := suresql.NewDatabase(config)
	if err != nil {
		simplelog.LogErrorAny("Main", err, "Failed to connect to database via SureSQL")
		return
	}

	simplelog.LogThis("Successfully connected via SureSQL abstraction!")
	simplelog.LogThis(fmt.Sprintf("Database driver: %s", suresql.CurrentNode.Status.DBMSDriver))

	status, err := db.Status()
	if err != nil {
		simplelog.LogErrorAny("Main", err, "Failed to get database status")
	} else {
		simplelog.LogThis(fmt.Sprintf("Database status: %+v", status))
	}

	// Run example operations
	simplelog.LogThis("\n--- Running Example Operations ---")

	// 1. Create table
	createTable(db)

	// 2. Insert data
	insertData(db)

	// 3. Select one record
	selectOne(db)

	// 4. Select many records
	selectMany(db)

	// 4b. Try complex nested conditions (may not be supported)
	selectManyComplex(db)

	// 5. Update data
	updateData(db)

	// 6. Delete data
	deleteData(db)

	simplelog.LogThis("\n=== Example completed successfully! ===")
}

// createTable creates the users table if it doesn't exist
func createTable(db orm.Database) {
	simplelog.LogThis("\n1. Creating users table...")

	sql := `CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		email VARCHAR(100) UNIQUE NOT NULL,
		age INTEGER,
		status VARCHAR(50)
	)`

	err := db.ExecOneSQL(sql)
	if err.Error != nil {
		simplelog.LogErrorAny("createTable", err.Error, "Failed to create table")
		return
	}

	simplelog.LogThis("Table 'users' created successfully!")
}

// insertData inserts sample users into the database
func insertData(db orm.Database) {
	simplelog.LogThis("\n2. Inserting sample data...")

	users := []User{
		{"Alice Johnson", "alice@example.com", 30, "active"},
		{"Bob Smith", "bob@example.com", 25, "active"},
		{"Charlie Brown", "charlie@example.com", 35, "pending"},
		{"Diana Prince", "diana@example.com", 28, "active"},
		{"Eve Anderson", "eve@example.com", 22, "new"},
	}

	var dbRecords []orm.DBRecord
	for _, user := range users {
		record, err := orm.TableStructToDBRecord(&user)
		if err != nil {
			simplelog.LogErrorAny("insertData", err, "Failed to convert user to DBRecord")
			continue
		}
		dbRecords = append(dbRecords, record)
	}

	_, err := db.InsertManyDBRecords(dbRecords, true)
	if err != nil {
		simplelog.LogErrorAny("insertData", err, "Failed to insert records")
		return
	}

	simplelog.LogThis(fmt.Sprintf("Successfully inserted %d users!", len(users)))
}

// selectOne demonstrates selecting a single record with raw SQL
func selectOne(db orm.Database) {
	simplelog.LogThis("\n3. Selecting one user (age > 25 and status = 'active')...")

	// Use raw SQL for reliable query execution
	sql := "SELECT * FROM users WHERE age > 25 AND status = 'active' ORDER BY name ASC LIMIT 1"

	records, err := db.SelectOneSQL(sql)
	if err != nil {
		simplelog.LogErrorAny("selectOne", err, "Failed to select user")
		return
	}

	if len(records) > 0 {
		simplelog.LogFormat("Found user: %+v\n", records[0].Data)
	} else {
		simplelog.LogThis("No user found matching criteria")
	}
}

// selectMany demonstrates selecting multiple records with raw SQL
func selectMany(db orm.Database) {
	simplelog.LogThis("\n4. Selecting multiple users (age > 20)...")

	// Use raw SQL for reliable query execution
	sqls := []string{"SELECT * FROM users WHERE age > 20 ORDER BY age DESC, name ASC LIMIT 10"}

	results, err := db.SelectManySQL(sqls)
	if err != nil {
		simplelog.LogErrorAny("selectMany", err, "Failed to select users")
		return
	}

	// SelectManySQL returns [][]DBRecord (results for each query)
	if len(results) > 0 {
		records := results[0] // Get results from first query
		simplelog.LogThis(fmt.Sprintf("Found %d users:", len(records)))
		for idx, record := range records {
			simplelog.LogFormat("  %d. %+v\n", idx+1, record.Data)
		}
	} else {
		simplelog.LogThis("No users found")
	}
}

// selectManyComplex demonstrates complex queries with raw SQL
func selectManyComplex(db orm.Database) {
	simplelog.LogThis("\n4b. Selecting users with complex conditions (age > 20 AND status active or pending)...")

	// Complex query with nested conditions using raw SQL
	sqls := []string{`SELECT * FROM users
	        WHERE age > 20
	        AND (status = 'active' OR status = 'pending')
	        ORDER BY age DESC, name ASC
	        LIMIT 10`}

	results, err := db.SelectManySQL(sqls)
	if err != nil {
		simplelog.LogErrorAny("selectManyComplex", err, "Failed to select users with complex conditions")
		return
	}

	// SelectManySQL returns [][]DBRecord (results for each query)
	if len(results) > 0 {
		records := results[0] // Get results from first query
		simplelog.LogThis(fmt.Sprintf("Found %d users:", len(records)))
		for idx, record := range records {
			simplelog.LogFormat("  %d. %+v\n", idx+1, record.Data)
		}
	} else {
		simplelog.LogThis("No users found")
	}
}

// updateData demonstrates updating records
func updateData(db orm.Database) {
	simplelog.LogThis("\n5. Updating user status (set pending users to active)...")

	sql := `UPDATE users SET status = 'active' WHERE status = 'pending'`

	err := db.ExecOneSQL(sql)
	if err.Error != nil {
		simplelog.LogErrorAny("updateData", err.Error, "Failed to update records")
		return
	}

	simplelog.LogThis("Successfully updated user statuses!")
}

// deleteData demonstrates deleting records
func deleteData(db orm.Database) {
	simplelog.LogThis("\n6. Deleting users with status 'new'...")

	sql := `DELETE FROM users WHERE status = 'new'`

	err := db.ExecOneSQL(sql)
	if err.Error != nil {
		simplelog.LogErrorAny("deleteData", err.Error, "Failed to delete records")
		return
	}

	simplelog.LogThis("Successfully deleted users with status 'new'!")
}
