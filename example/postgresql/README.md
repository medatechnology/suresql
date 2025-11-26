# PostgreSQL Example for SureSQL

This example demonstrates how to use the **SureSQL abstraction layer** with PostgreSQL as the database backend, showcasing database-agnostic operations through the SureSQL interface.

## What is SureSQL Abstraction?

SureSQL provides a **database abstraction layer** that allows you to switch between different database backends (RQLite, PostgreSQL, MySQL, etc.) without changing your application code. You simply:

1. Set the `DBMS_TYPE` environment variable to your desired database
2. Use `suresql.NewDatabase()` which automatically selects the right driver
3. All database operations work the same regardless of the backend

This example shows PostgreSQL as the backend, but you could switch to RQLite by just changing `DBMS_TYPE=RQLITE` in your `.env` file!

## Features Demonstrated

- PostgreSQL database connection
- Table creation
- CRUD operations (Create, Read, Update, Delete)
- Complex query conditions with nested logic
- Struct-to-database record conversion
- Order by, limit, and offset clauses

## Prerequisites

Before running this example, make sure you have:

1. Go 1.23.2 or higher installed
2. PostgreSQL server running (locally or remotely)
3. A PostgreSQL database created

## PostgreSQL Setup

### Option 1: Using Docker (Recommended for testing)

```bash
# Start PostgreSQL container
docker run --name postgres-test \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=testdb \
  -p 5432:5432 \
  -d postgres:latest

# Check if container is running
docker ps | grep postgres-test
```

### Option 2: Using Local PostgreSQL Installation

If you have PostgreSQL installed locally:

```bash
# Create a new database
createdb testdb

# Or using psql
psql -U postgres
CREATE DATABASE testdb;
\q
```

## Installation

1. Navigate to the example directory:
```bash
cd example/postgresql
```

2. Copy the environment sample file:
```bash
cp .env.sample .env
```

3. Edit the `.env` file with your PostgreSQL credentials:
```bash
# IMPORTANT: Set DBMS_TYPE to use PostgreSQL
DBMS_TYPE=POSTGRESQL

# Update these values to match your PostgreSQL setup
DBMS_HOST=localhost
DBMS_PORT=5432
DBMS_USERNAME=postgres
DBMS_PASSWORD=password
DBMS_DATABASE=testdb
DBMS_SSL=false
```

**Note**: The key advantage of SureSQL abstraction is that you can change `DBMS_TYPE` to `RQLITE` and the same code will work with RQLite instead!

4. Initialize Go modules (if not already done):
```bash
go mod init example-postgresql
go mod tidy
```

## Running the Example

```bash
# Run the example
go run main.go models.go
```

## What the Example Does

The example performs the following operations in sequence:

### 1. Create Table
Creates a `users` table with the following schema:
- `id` (SERIAL PRIMARY KEY)
- `name` (VARCHAR)
- `email` (VARCHAR, UNIQUE)
- `age` (INTEGER)
- `status` (VARCHAR)

### 2. Insert Data
Inserts 5 sample users into the database:
- Alice Johnson (30, active)
- Bob Smith (25, active)
- Charlie Brown (35, pending)
- Diana Prince (28, active)
- Eve Anderson (22, new)

### 3. Select One Record
Demonstrates selecting a single user with a simple condition:
- Age > 25
- Ordered by name ascending

### 4. Select Multiple Records
Demonstrates selecting users with simple conditions:
- Age > 20
- Ordered by age descending, then name ascending
- Limited to 10 records

**Note**: The example also includes a complex nested condition test (`selectManyComplex`) which may not work if the ORM doesn't fully support nested conditions yet. In such cases, use raw SQL queries instead.

### 5. Update Data
Updates all users with 'pending' status to 'active'

### 6. Delete Data
Deletes all users with 'new' status

## Code Structure

### main.go
Contains the main application logic and example functions demonstrating various database operations.

### models.go
Defines the data models (structs) used in the example:
- `User` - Represents a user entity
- `Product` - Example product entity (for reference)
- `Order` - Example order entity (for reference)

Each model includes:
- Struct tags for database field mapping (`db:"field_name"`)
- `TableName()` method to specify the database table name

## Using SureSQL Abstraction

### Creating a Database Connection

The key feature of SureSQL is the abstraction layer. Instead of directly using a specific database driver:

```go
// Use SureSQL abstraction (database-agnostic)
config := suresql.SureSQLDBMSConfig{
    DBMS:     "POSTGRESQL",  // Or "RQLITE", "MYSQL", etc.
    Host:     "localhost",
    Port:     "5432",
    Username: "postgres",
    Password: "password",
    Database: "testdb",
    SSL:      false,
}

// This automatically selects the right driver!
db, err := suresql.NewDatabase(config)
```

The same code works with any supported database - just change the `DBMS` field!

### Converting Structs to DBRecord

```go
user := User{
    Name:   "John Doe",
    Email:  "john@example.com",
    Age:    30,
    Status: "active",
}

record, err := orm.TableStructToDBRecord(&user)
if err != nil {
    // Handle error
}
```

### Building Simple Conditions

For simple queries, use the Condition struct:

```go
// Simple condition
condition := orm.Condition{
    Field:    "age",
    Operator: ">",
    Value:    25,
    OrderBy:  []string{"name ASC"},
    Limit:    10,
    Offset:   0,
}
```

### Complex Queries with Raw SQL

For complex queries with nested conditions, use raw SQL for better control:

```go
// Complex query with raw SQL
sql := `SELECT * FROM users
        WHERE age > 20
        AND (status = 'active' OR status = 'pending')
        ORDER BY age DESC, name ASC
        LIMIT 10`

results, err := db.SelectManySQL(sql)
```

**Note**: Complex nested conditions using `orm.Condition` may not be fully supported in all database drivers yet. For complex queries, raw SQL is recommended.

### Executing Queries

```go
// Select one record
record, err := db.SelectOneWithCondition("users", &condition)

// Select multiple records
records, err := db.SelectManyWithCondition("users", &condition)

// Execute raw SQL
err := db.ExecOneSQL("UPDATE users SET status = 'active' WHERE id = 1")

// Insert records
_, err := db.InsertManyDBRecords(dbRecords, true)
```

## Expected Output

```
=== SureSQL PostgreSQL Example ===
This example demonstrates using SureSQL abstraction with PostgreSQL backend
Connecting to database via SureSQL abstraction...
Config: DBMS=POSTGRESQL, Host=localhost, Port=5432, User=postgres, Database=testdb
Successfully connected via SureSQL abstraction!
Database driver: postgres

--- Running Example Operations ---

1. Creating users table...
Table 'users' created successfully!

2. Inserting sample data...
Successfully inserted 5 users!

3. Selecting one user (age > 25 and status = 'active')...
Found user: map[age:30 email:alice@example.com id:1 name:Alice Johnson status:active]

4. Selecting multiple users (age > 20 and status active or pending)...
Found 4 users:
  1. map[age:35 email:charlie@example.com id:3 name:Charlie Brown status:pending]
  2. map[age:30 email:alice@example.com id:1 name:Alice Johnson status:active]
  3. map[age:28 email:diana@example.com id:4 name:Diana Prince status:active]
  4. map[age:25 email:bob@example.com id:2 name:Bob Smith status:active]

5. Updating user status (set pending users to active)...
Successfully updated user statuses!

6. Deleting users with status 'new'...
Successfully deleted users with status 'new'!

=== Example completed successfully! ===
```

## Troubleshooting

### Connection Refused
If you get a "connection refused" error:
- Make sure PostgreSQL is running
- Check the host and port in your `.env` file
- Verify firewall settings aren't blocking the connection

### Authentication Failed
If you get an authentication error:
- Verify the username and password in your `.env` file
- Check PostgreSQL's `pg_hba.conf` for authentication settings

### Database Does Not Exist
If the database doesn't exist:
```bash
createdb testdb
```

### SSL Mode Issues
If you encounter SSL-related errors:
- Set `POSTGRES_SSLMODE=disable` for local development
- For production, use `require` or higher

## Cleaning Up

### Stop and remove Docker container
```bash
docker stop postgres-test
docker rm postgres-test
```

### Drop the test database
```bash
dropdb testdb
```

## Additional Resources

- [SureSQL Documentation](../../README.md)
- [SimpleORM GitHub Repository](https://github.com/medatechnology/simpleorm)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)

## License

This example is part of the SureSQL project and follows the same license.
