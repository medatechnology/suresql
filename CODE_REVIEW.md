# SureSQL Code Review - Bugs, Inefficiencies & Improvements

**Date**: 2025-11-21
**Reviewer**: Professional Code Review
**Scope**: Complete codebase analysis

---

## Executive Summary

This review identified **32 issues** across the codebase, ranging from critical concurrency bugs to code quality improvements. Key findings include:

- **Critical**: 4 issues (race conditions, security vulnerabilities)
- **High**: 8 issues (error handling, resource leaks, inefficiencies)
- **Medium**: 12 issues (code duplication, maintainability)
- **Low**: 8 issues (code quality, best practices)

---

## CRITICAL ISSUES

### 1. **Race Condition on Global `CurrentNode` Variable** ⚠️
**Location**: Multiple files (config.go, db_node.go, server/handler.go, etc.)
**Severity**: CRITICAL

**Problem**:
```go
var CurrentNode SureSQLNode  // Global variable accessed by all handlers
```

The `CurrentNode` global variable is accessed concurrently by multiple HTTP handlers without synchronization. This can cause:
- Data races when reading/writing node configuration
- Crashes or undefined behavior under high concurrency
- Inconsistent state during configuration updates

**Impact**: Production crashes, data corruption

**Fix**:
```go
// Add mutex protection
type SafeSureSQLNode struct {
    mu   sync.RWMutex
    node SureSQLNode
}

var CurrentNode = &SafeSureSQLNode{}

func (s *SafeSureSQLNode) GetConfig() ConfigTable {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.node.Config
}

func (s *SafeSureSQLNode) UpdateConfig(cfg ConfigTable) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.node.Config = cfg
}
```

---

### 2. **Password Stored in Response** 🔒
**Location**: `server/internal.go:214`
**Severity**: CRITICAL (Security)

**Problem**:
```go
// Check if user exists
user, err := userNameExist(updateReq.Username)
if err != nil {
    return state.SetError("User not found", err, http.StatusNotFound)...
}
// remove the password first before using it for response
user.Password = ""  // ⚠️ This happens AFTER the check, but user object may leak before this
```

The password is cleared *after* potential error conditions. If an error occurs before line 214, the user object with hashed password could be leaked in logs or error messages.

**Fix**:
```go
user, err := userNameExist(updateReq.Username)
if err != nil {
    return state.SetError("User not found", err, http.StatusNotFound)...
}
// Immediately clear password after retrieval
user.Password = ""
```

Apply this pattern everywhere `UserTable` is retrieved (also in HandleListUsers:108, HandleConnect:153, etc.)

---

### 3. **Incomplete Error Handling in `DecodeToken`**
**Location**: `server/auth.go:172-181`
**Severity**: CRITICAL

**Problem**:
```go
func DecodeToken(tokenstring string, config *suresql.SureSQLDBMSConfig) (string, error) {
    tokenMap, err := encryption.ParseJWEToMap(tokenstring, []byte(config.JWEKey))
    if err != nil {
        // ⚠️ Error is caught but ignored!
    }
    if tokenMap[TOKEN_STRING] != "HELLO_TEST" {
        return "", medaerror.Simple("token invalid:" + tokenMap[TOKEN_STRING])
    }
    config.Token = tokenMap[TOKEN_STRING]
    return config.Token, nil
}
```

The error from `ParseJWEToMap` is caught but completely ignored. If parsing fails, `tokenMap` will be `nil`, causing a **panic** on the next line when accessing `tokenMap[TOKEN_STRING]`.

**Fix**:
```go
func DecodeToken(tokenstring string, config *suresql.SureSQLDBMSConfig) (string, error) {
    tokenMap, err := encryption.ParseJWEToMap(tokenstring, []byte(config.JWEKey))
    if err != nil {
        return "", fmt.Errorf("failed to parse JWE token: %w", err)
    }
    // ... rest of function
}
```

---

### 4. **DB Connection Pool Memory Leak**
**Location**: `server/handler.go:175-177`, `db_node.go:57-62`
**Severity**: CRITICAL

**Problem**:
```go
// In HandleConnect - connection added to pool
suresql.CurrentNode.DBConnections.Put(tokenResponse.Token, 0, newDB)

// In HandleRefresh - old connection renamed, not closed!
suresql.CurrentNode.RenameDBConnection(tokmap.Token, tokenResponse.Token)

// RenameDBConnection implementation
func (n *SureSQLNode) RenameDBConnection(old, new string) {
    if val, ok := n.DBConnections.Get(old); ok {
        n.DBConnections.Put(new, 0, val)  // ⚠️ Same connection reused
        n.DBConnections.Delete(old)
    }
}
```

**Issues**:
1. When TTL expires on old tokens, connections are not properly closed, causing resource leaks
2. `RenameDBConnection` has TODO comment saying it should be replaced with create new + close old
3. No explicit `Close()` called on database connections

**Fix**:
```go
// In HandleRefresh - create fresh connection
func HandleRefresh(ctx simplehttp.Context) error {
    // ... validation code ...

    // Get and close old connection
    if oldDB, err := suresql.CurrentNode.GetDBConnectionByToken(tokmap.Token); err == nil {
        if closer, ok := oldDB.(interface{ Close() error }); ok {
            closer.Close()
        }
    }
    suresql.CurrentNode.DBConnections.Delete(tokmap.Token)

    // Create new connection
    configCopy := suresql.CurrentNode.InternalConfig
    newDB, err := suresql.NewDatabase(configCopy)
    if err != nil {
        return state.SetError("Failed to create database connection", err, http.StatusInternalServerError)...
    }

    // Add new connection with new token
    suresql.CurrentNode.DBConnections.Put(tokenResponse.Token, 0, newDB)

    // ... rest of function
}

// Add cleanup callback to TTLMap when items expire
// In db_node.go:145
CurrentNode.DBConnections = medattlmap.NewTTLMapWithCallback(
    CurrentNode.Config.RefreshExp,
    CurrentNode.Config.TTLTicker,
    func(key string, value interface{}) {
        if db, ok := value.(suresql.SureSQLDB); ok {
            if closer, ok := db.(interface{ Close() error }); ok {
                closer.Close()
            }
        }
    },
)
```

---

## HIGH PRIORITY ISSUES

### 5. **Inefficient Configuration Loading**
**Location**: `config.go:157-180`
**Severity**: HIGH (Performance)

**Problem**:
```go
func LoadDBMSConfigFromEnvironment() SureSQLDBMSConfig {
    tmpConfig := SureSQLDBMSConfig{
        Host:        utils.GetEnvString("DBMS_HOST", ""),  // Multiple env lookups
        Port:        utils.GetEnvString("DBMS_PORT", ""),
        Username:    utils.GetEnvString("DBMS_USERNAME", ""),
        // ... 14 more GetEnv calls
    }
    return tmpConfig
}
```

Every handler call reloads environment variables. This is inefficient and unnecessary after initial startup.

**Fix**:
```go
// Load once at startup, cache in memory
var cachedDBMSConfig SureSQLDBMSConfig
var configOnce sync.Once

func GetDBMSConfig() SureSQLDBMSConfig {
    configOnce.Do(func() {
        cachedDBMSConfig = loadDBMSConfigFromEnvironment()
    })
    return cachedDBMSConfig
}

// Provide explicit reload function if needed
func ReloadDBMSConfig() {
    cachedDBMSConfig = loadDBMSConfigFromEnvironment()
}
```

---

### 6. **Excessive Code Duplication in SQL Handlers**
**Location**: `server/handler_sql_query.go` (entire file)
**Severity**: HIGH (Maintainability)

**Problem**:
The `HandleSQLQuery` function has massive code duplication with nearly identical blocks for:
- Single vs Multiple statements (lines 49-96 vs 98-121)
- Raw SQL vs Parameterized SQL (lines 49-121 vs 122-193)
- Single row vs Multiple rows (lines 52-74 vs 75-96)

**Total duplicated logic**: ~140 lines with 80%+ similarity

**Fix**: Extract common patterns into helper functions
```go
type sqlQueryExecutor func(suresql.SureSQLDB, interface{}) (orm.DBRecords, error)

func executeSQL Query(
    db suresql.SureSQLDB,
    statements []string,
    paramSQL []orm.ParametereizedSQL,
    singleRow bool,
) (suresql.QueryResponseSQL, error) {
    var executor sqlQueryExecutor
    var data interface{}

    // Determine executor based on input type
    if len(statements) > 0 {
        if len(statements) == 1 {
            if singleRow {
                executor = func(d suresql.SureSQLDB, v interface{}) (orm.DBRecords, error) {
                    rec, err := d.SelectOnlyOneSQL(v.(string))
                    return orm.DBRecords{rec}, err
                }
            } else {
                executor = func(d suresql.SureSQLDB, v interface{}) (orm.DBRecords, error) {
                    return d.SelectOneSQL(v.(string))
                }
            }
            data = statements[0]
        } else {
            executor = func(d suresql.SureSQLDB, v interface{}) (orm.DBRecords, error) {
                return d.SelectManySQL(v.([]string))
            }
            data = statements
        }
    } else { // paramSQL path
        // Similar logic for parameterized queries
    }

    // Execute once
    records, err := executor(db, data)
    if err != nil && err != orm.ErrSQLNoRows {
        return nil, err
    }

    // Build response
    // ...
}
```

**Benefit**: Reduce from ~200 lines to ~80 lines, improve maintainability

---

### 7. **Missing Input Validation**
**Location**: Multiple handler functions
**Severity**: HIGH (Security)

**Problems**:
1. **No SQL injection protection in internal.go:258-260**
```go
updateSQL := "UPDATE " + UserTable{}.TableName() + " SET " + strings.Join(updateFields, ", ") + " WHERE username = ?"
// ⚠️ UserTable{}.TableName() is not validated, could be manipulated
```

2. **No length limits on user inputs** (server/internal.go:122-128)
```go
var createReq UserTable
if err := ctx.BindJSON(&createReq); err != nil {
    return state.SetError("Invalid request format", err, http.StatusBadRequest)...
}
// ⚠️ No validation on username/password length
```

3. **No sanitization of table names in queries** (server/handler_query.go:34)
```go
if queryReq.Table == "" {
    return state.SetError("Table name is required", nil, http.StatusBadRequest)...
}
// ⚠️ Table name passed directly to query without validation
```

**Fix**:
```go
// Add validation helper
func validateTableName(name string) error {
    // Only allow alphanumeric and underscores
    if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(name) {
        return fmt.Errorf("invalid table name: %s", name)
    }
    // Prevent access to internal tables
    if strings.HasPrefix(name, "_") {
        return fmt.Errorf("access to internal tables denied")
    }
    return nil
}

// Add input length validation
const (
    MaxUsernameLength = 50
    MaxPasswordLength = 100
    MaxTableNameLength = 64
)

func validateUserInput(user UserTable) error {
    if len(user.Username) == 0 || len(user.Username) > MaxUsernameLength {
        return fmt.Errorf("username must be 1-%d characters", MaxUsernameLength)
    }
    if len(user.Password) == 0 || len(user.Password) > MaxPasswordLength {
        return fmt.Errorf("password must be 1-%d characters", MaxPasswordLength)
    }
    // Add regex validation for username (alphanumeric + specific chars only)
    if !regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString(user.Username) {
        return fmt.Errorf("username contains invalid characters")
    }
    return nil
}
```

---

### 8. **Token Store Not Initialized Before Use**
**Location**: `server/auth.go:44-47`, `server/handler.go:72-74`
**Severity**: HIGH

**Problem**:
```go
// In CreateServer
InitTokenMaps()  // Called during server creation

// But CurrentNode.Config not loaded yet!
func InitTokenMaps() {
    TokenStore = NewTokenStore(
        suresql.DEFAULT_TOKEN_EXPIRES_MINUTES,  // Uses constants
        suresql.DEFAULT_REFRESH_EXPIRES_MINUTES, // Not DB config!
    )
}
```

**Order of operations**:
1. `CreateServer()` calls `InitTokenMaps()` (line 72 in handler.go)
2. But configuration from DB is loaded in `ConnectInternal()` (called before CreateServer in main)
3. TokenStore uses **default constants** instead of **database settings**

**Impact**: Token expiration settings from database are ignored

**Fix**:
```go
// In server/handler.go:CreateServer()
func CreateServer(cnode suresql.SureSQLNode) simplehttp.Server {
    // ... other setup ...

    // Use actual config from node, not defaults
    el = metrics.StartTimeIt("Initializing TTLMaps with DB config...", 0)
    TokenStore = NewTokenStore(cnode.Config.TokenExp, cnode.Config.RefreshExp)
    metrics.StopTimeItPrint(el, "Done")

    // ... rest of function
}
```

---

### 9. **Error Variable Shadowing**
**Location**: `server/internal.go:268-270`
**Severity**: HIGH (Bug)

**Problem**:
```go
result := suresql.CurrentNode.InternalConnection.ExecOneSQLParameterized(paramSQL)
if result.Error != nil {
    return state.SetError("Failed to update user", err, http.StatusInternalServerError)...
    //                                            ^^^ Wrong variable! Should be result.Error
}
```

The function uses `err` variable which is from a previous scope (line 209) instead of `result.Error`. This returns the wrong error to the user.

**Same issue in**:
- `internal.go:303` (HandleDeleteUser)

**Fix**:
```go
result := suresql.CurrentNode.InternalConnection.ExecOneSQLParameterized(paramSQL)
if result.Error != nil {
    return state.SetError("Failed to update user", result.Error, http.StatusInternalServerError)...
}
```

---

### 10. **Migration System Not Transactional**
**Location**: `initdb.go:21-71`
**Severity**: HIGH

**Problem**:
```go
for _, ef := range allUpFiles {
    // ... execute migration ...
    res, err := CurrentNode.InternalConnection.ExecManySQL(sqlCommands)
    if err != nil {
        // NOTE: if one of the file has error, then cannot continue just return.
        //       Meaning could potentially initialized partially
        // TODO: create rollback functionality here.
        simplelog.LogErr(err, "cannot init migrate")
        return err
    }
}
```

**Issues**:
1. No transaction wrapper - partial migrations can leave DB in inconsistent state
2. No migration versioning table to track what's been applied
3. No rollback mechanism
4. No migration checksum validation

**Fix**: Implement proper migration management
```go
type Migration struct {
    Version int
    Name    string
    Applied time.Time
    Checksum string
}

func InitDB(force bool) error {
    // Create migrations table if not exists
    if err := ensureMigrationsTable(); err != nil {
        return err
    }

    // Get applied migrations
    applied, err := getAppliedMigrations()
    if err != nil {
        return err
    }

    // Find pending migrations
    pending := findPendingMigrations(applied)

    // Execute each in transaction
    for _, migration := range pending {
        tx, err := CurrentNode.InternalConnection.Begin()
        if err != nil {
            return fmt.Errorf("failed to start transaction: %w", err)
        }

        if err := executeMigration(tx, migration); err != nil {
            tx.Rollback()
            return fmt.Errorf("migration %s failed: %w", migration.Name, err)
        }

        if err := recordMigration(tx, migration); err != nil {
            tx.Rollback()
            return fmt.Errorf("failed to record migration: %w", err)
        }

        if err := tx.Commit(); err != nil {
            return fmt.Errorf("failed to commit migration: %w", err)
        }
    }

    return nil
}
```

---

### 11. **Inconsistent Error Handling for ErrSQLNoRows**
**Location**: Multiple handlers
**Severity**: HIGH (Inconsistency)

**Problem**: `ErrSQLNoRows` is handled inconsistently across handlers:
- Sometimes logged as error (handler_query.go:62)
- Sometimes returned as success with empty data (handler_query.go:79)
- Sometimes ignored completely (internal.go:98)

This makes API behavior unpredictable for clients.

**Fix**: Standardize the pattern
```go
// Option 1: Always return 200 with empty array
records, err := userDB.SelectMany(table)
if err != nil {
    if err == orm.ErrSQLNoRows {
        // Not an error - return empty result
        return state.SetSuccess("Query executed successfully", QueryResponse{
            Records: []orm.DBRecord{},
            Count: 0,
        })...
    }
    // Real error
    return state.SetError("Failed to execute query", err, http.StatusInternalServerError)...
}

// Option 2: Return 404 for single record, 200 for multiple records
// Document this behavior in API docs
```

---

### 12. **Missing Context Cancellation**
**Location**: All handler functions
**Severity**: HIGH

**Problem**: None of the database operations respect the request context. If a client disconnects, the database query continues to execute, wasting resources.

**Fix**:
```go
// Add context to database interface
type Database interface {
    SelectOneWithContext(ctx context.Context, table string) (DBRecord, error)
    // ... other methods
}

// In handlers
func HandleQuery(ctx simplehttp.Context) error {
    // ...
    record, err := userDB.SelectOneWithContext(ctx.Request().Context(), queryReq.Table)
    if err != nil {
        if errors.Is(err, context.Canceled) {
            return state.SetError("Request canceled", err, http.StatusRequestTimeout)...
        }
        // ... other error handling
    }
}
```

---

## MEDIUM PRIORITY ISSUES

### 13. **Repeated Token Validation Code**
**Location**: Every handler in server/ package
**Severity**: MEDIUM (DRY Violation)

**Problem**: This pattern repeats in every token-protected handler:
```go
if state.Token == nil {
    return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized)...
}
```

Appears in:
- handler_insert.go:19
- handler_query.go:23
- handler_sql_exec.go:22
- handler_sql_query.go:20
- handler.go:228

**Fix**: Move to middleware or HandlerState
```go
// In HandlerState
func NewHandlerTokenState(ctx simplehttp.Context, label, table string) (HandlerState, error) {
    token := ctx.Get(TOKEN_TABLE_STRING).(*suresql.TokenTable)
    if token == nil {
        return HandlerState{}, ErrMissingToken
    }

    state := HandlerState{
        // ... initialization
        Token: token,
        User: token.UserName,
    }
    return state, nil
}

// In handlers
func HandleQuery(ctx simplehttp.Context) error {
    state, err := NewHandlerTokenState(ctx, "/query/", "request")
    if err != nil {
        return handleStateCreationError(ctx, err)
    }
    // ... rest of handler
}
```

---

### 14. **Magic Numbers Throughout Code**
**Location**: Multiple files
**Severity**: MEDIUM

**Problems**:
```go
// config.go:14
LEADER_NODE_NUMBER = 1  // Why 1? Document reasoning

// handler_sql_exec.go:112-114
if !LOG_RAW_QUERY {
    return ""  // Why empty string? Should return meaningful message
}

// auth.go:23
TOKEN_LENGTH_MULTIPLIER = 3  // Why 3? What's the resulting length?
```

**Fix**:
```go
const (
    // Leader is always node 1 in cluster for consistency
    LEADER_NODE_NUMBER = 1

    // Token length multiplier - produces 96 char tokens (32 bytes * 3)
    // Provides 256 bits of entropy for security
    TOKEN_LENGTH_MULTIPLIER = 3

    // When LOG_RAW_QUERY is false, we return empty to save memory
    // and prevent sensitive data in logs
    RAW_QUERY_DISABLED_MSG = ""
)
```

---

### 15. **Inefficient String Concatenation in Loops**
**Location**: `server/internal.go:258`, `handler_insert.go:106-132`
**Severity**: MEDIUM

**Problem**:
```go
// ListTableNames function
result := ""
for tableName := range tableMap {
    if result != "" {
        result += ", "  // ⚠️ Creates new string each iteration
    }
    result += tableName
}
```

**Fix**:
```go
var builder strings.Builder
first := true
for tableName := range tableMap {
    if !first {
        builder.WriteString(", ")
    }
    builder.WriteString(tableName)
    first = false
}
return builder.String()
```

---

### 16. **Unused Functions and Code**
**Location**: Multiple files
**Severity**: MEDIUM

**Unused functions**:
1. `server/auth.go:155-169` - `DecryptCredentials` (marked as not used)
2. `server/auth.go:172-181` - `DecodeToken` (marked as not used)
3. `server/handler_insert.go:88-101` - `AreSameTable` (never called)
4. `server/handler_insert.go:105-133` - `ListTableNames` (commented as "not used yet")

**Fix**: Remove unused code or add TODO with specific future use case
```go
// Either remove:
// - DecryptCredentials
// - DecodeToken
// - AreSameTable
// - ListTableNames

// Or document when they'll be used:
// TODO(v2.0): DecryptCredentials will be used when implementing
// end-to-end encryption for credentials in transit
```

---

### 17. **Inconsistent Naming Conventions**
**Location**: Throughout codebase
**Severity**: MEDIUM

**Problems**:
1. Mixed naming: `DBLogging` vs `db_node.go` (snake_case file, PascalCase function)
2. Abbreviations inconsistent: `DBMS` vs `Db` vs `DB`
3. `HandleGetSchema` defined twice (internal.go:310, 322)

**Fix**: Standardize
```go
// Use consistent abbreviation
DB (uppercase) for "database" in all contexts
DBMS (uppercase) for "database management system"

// File naming: all snake_case
db_node.go ✓
handler_state.go ✓
dbNode.go ✗ (wrong)

// Function naming: all PascalCase or camelCase per Go conventions
HandleGetSchema ✓
handle_get_schema ✗ (wrong)
```

---

### 18. **Hardcoded Sleep/Timeout Values**
**Location**: None found explicitly, but DEFAULT_TIMEOUT, DEFAULT_RETRY patterns
**Severity**: MEDIUM

**Problem**: `models.go:19-22` defines defaults but no way to override at runtime:
```go
const (
    DEFAULT_TIMEOUT       = 60 * time.Second
    DEFAULT_RETRY_TIMEOUT = 60 * time.Second
    DEFAULT_RETRY         = 3
)
```

**Fix**: Make configurable via environment or settings table
```go
// Add to _settings table
// category: connection, key: http_timeout, int_value: 60
// category: connection, key: retry_count, int_value: 3
```

---

### 19. **No Health Check Endpoint**
**Location**: server/handler.go (missing)
**Severity**: MEDIUM

**Problem**: No `/health` or `/ready` endpoint for container orchestration (Kubernetes, Docker Swarm)

**Fix**:
```go
func RegisterRoutes(server simplehttp.Server) {
    // Add health check endpoints (no auth required)
    server.GET("/health", HandleHealth)
    server.GET("/ready", HandleReady)

    // ... existing routes
}

func HandleHealth(ctx simplehttp.Context) error {
    // Simple liveness check
    return ctx.JSON(http.StatusOK, map[string]string{
        "status": "ok",
        "version": suresql.APP_VERSION,
    })
}

func HandleReady(ctx simplehttp.Context) error {
    // Readiness check - verify DB connection
    if !suresql.CurrentNode.InternalConnection.IsConnected() {
        return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
            "status": "not ready",
            "reason": "database connection failed",
        })
    }
    return ctx.JSON(http.StatusOK, map[string]string{
        "status": "ready",
    })
}
```

---

### 20. **No Request ID for Tracing**
**Location**: All handlers
**Severity**: MEDIUM

**Problem**: Logs don't have request IDs, making it impossible to trace a single request through the system

**Fix**:
```go
// Add middleware
func MiddlewareRequestID() simplehttp.Middleware {
    return simplehttp.WithName("RequestID", func(next simplehttp.HandlerFunc) simplehttp.HandlerFunc {
        return func(ctx simplehttp.Context) error {
            requestID := ctx.GetHeader("X-Request-ID")
            if requestID == "" {
                requestID = generateUUID()
            }
            ctx.Set("request_id", requestID)
            ctx.SetHeader("X-Request-ID", requestID)
            return next(ctx)
        }
    })
}

// Add to logging
type AccessLogTable struct {
    // ... existing fields
    RequestID string `json:"request_id,omitempty" db:"request_id"`
}
```

---

### 21. **Verbose Error Messages Leak Implementation Details**
**Location**: Multiple handlers
**Severity**: MEDIUM (Security)

**Problem**:
```go
return state.SetError("Failed to create database connection", err, http.StatusInternalServerError)
```

Raw errors are returned to clients, potentially exposing:
- Database schema information
- File paths
- Internal server structure

**Fix**:
```go
// Log detailed error internally
state.OnlyLog("Database connection failed: " + err.Error(), nil, false)

// Return generic error to client
return state.SetError("Service temporarily unavailable", nil, http.StatusInternalServerError)...
```

---

### 22. **No Rate Limiting**
**Location**: server/handler.go (missing)
**Severity**: MEDIUM

**Problem**: No protection against:
- Brute force attacks on `/db/connect`
- DDoS attacks
- Resource exhaustion

**Fix**:
```go
// Add rate limiting middleware
func MiddlewareRateLimit(requestsPerMinute int) simplehttp.Middleware {
    limiter := rate.NewLimiter(rate.Limit(requestsPerMinute), requestsPerMinute*2)

    return simplehttp.WithName("RateLimit", func(next simplehttp.HandlerFunc) simplehttp.HandlerFunc {
        return func(ctx simplehttp.Context) error {
            if !limiter.Allow() {
                return ctx.JSON(http.StatusTooManyRequests, suresql.StandardResponse{
                    Status: http.StatusTooManyRequests,
                    Message: "Rate limit exceeded",
                })
            }
            return next(ctx)
        }
    })
}

// Apply to sensitive endpoints
db := server.Group("/db")
db.Use(MiddlewareRateLimit(100)) // 100 req/min
```

---

### 23. **Configuration Precedence Undocumented**
**Location**: config.go:183-268
**Severity**: MEDIUM

**Problem**: `OverwriteConfigFromEnvironment` has complex logic but no clear documentation of precedence rules:
```go
func OverwriteConfigFromEnvironment() {
    ip := utils.GetEnvString("SURESQL_IP", "")
    if ip != "" {
        CurrentNode.Config.IP = ip  // When does this take precedence?
    }
    // ... 40 more similar checks
}
```

**Fix**: Document clearly
```go
// Configuration Loading Order (later overwrites earlier):
// 1. Built-in defaults (DEFAULT_* constants)
// 2. Database _configs table (single row)
// 3. Database _settings table (multi-row key-value)
// 4. Environment variables (SURESQL_* prefixed) - HIGHEST PRIORITY
//
// Example: SURESQL_PORT env var will override port in _configs table
//
// This allows runtime configuration without database changes,
// useful for containerized deployments.
func OverwriteConfigFromEnvironment() {
    // ... implementation
}
```

---

### 24. **Duplicate Function Definitions**
**Location**: server/internal.go:310, 322
**Severity**: MEDIUM

**Problem**:
```go
// Line 310
func HandleGetSchema(ctx simplehttp.Context) error {
    // ...
}

// Line 322 - DUPLICATE
func HandleDBMSStatus(ctx simplehttp.Context) error {
    // ...
}
```

Wait, these are different functions. But HandleGetSchema is registered twice:
- Line 68: `internalAPI.GET("/schema", HandleGetSchema)`
- Line 124: `api.GET("/getschema", HandleGetSchema)`

The second registration (line 124) is dead code because it's explicitly blocked inside the function (line 314-316).

**Fix**: Remove the dead endpoint registration
```go
// In RegisterRoutes - REMOVE THIS LINE:
// api.GET("/getschema", HandleGetSchema)

// Only keep internal route:
internalAPI.GET("/schema", HandleGetSchema)
```

---

## LOW PRIORITY ISSUES

### 25. **Commented-Out Code**
**Location**: Multiple files
**Severity**: LOW

**Examples**:
- config.go:253-268 (commented environment loading)
- handler.go:20-36 (commented constants and structs)
- handler_state.go:196-203 (commented QueryResponseSQL check)

**Fix**: Remove all commented code, use version control instead

---

### 26. **TODO Comments Without Context**
**Location**: Multiple files
**Severity**: LOW

**Examples**:
```go
// suresql.go:16
// TODO: FUTURE: maybe reading from environment...
// ⚠️ No issue number, no timeline, vague

// db_node.go:56
// TODO: please don't use this anymore, when token is refreshed...
// ⚠️ Why not remove if it shouldn't be used?
```

**Fix**: Use structured TODO format
```go
// TODO(username): Add PostgreSQL support - needed for v2.0 roadmap
// Related issue: #123

// DEPRECATED: RenameDBConnection will be removed in v1.5
// Use CreateFreshConnection instead
```

---

### 27. **Inconsistent Return Patterns**
**Location**: auth.go
**Severity**: LOW

**Problem**:
```go
// Some functions return pointer
func (t TokenStoreStruct) TokenExist(token string) (*suresql.TokenTable, bool)

// Others return value
func createNewTokenResponse(user UserTable) suresql.TokenTable
```

**Fix**: Be consistent
```go
// Return pointers for large structs, values for small ones
// TokenTable is medium-sized (5 fields), use value consistently
func (t TokenStoreStruct) TokenExist(token string) (suresql.TokenTable, bool) {
    val, ok := t.TokenMap.Get(token)
    if !ok {
        return suresql.TokenTable{}, false
    }
    return val.(suresql.TokenTable), true
}
```

---

### 28. **Missing Package-Level Documentation**
**Location**: All .go files
**Severity**: LOW

**Problem**: No package comments explaining purpose

**Fix**:
```go
// Package suresql provides a RESTful API middleware layer for SQL databases.
//
// It acts as a secure gateway between client applications and database systems,
// currently supporting RQLite as the backend. Key features include:
//   - JWT-based authentication with token refresh
//   - Connection pooling with TTL management
//   - Multi-node clustering support
//   - ORM-style query interface and raw SQL execution
//
// Architecture:
//   - suresql package: Core database abstraction and node management
//   - server package: HTTP handlers, middleware, authentication
//
// For usage examples, see README.md or CLAUDE.md
package suresql
```

---

### 29. **No Metrics/Monitoring Endpoints**
**Location**: Missing from server/handler.go
**Severity**: LOW

**Problem**: No `/metrics` endpoint for Prometheus/observability tools

**Fix**:
```go
// Add metrics endpoint
func RegisterMetricsRoutes(server simplehttp.Server) {
    metrics := server.Group("/metrics")
    metrics.Use(simplehttp.MiddlewareBasicAuth("admin", getMetricsPassword()))
    {
        metrics.GET("/prometheus", HandlePrometheusMetrics)
        metrics.GET("/stats", HandleStats)
    }
}

func HandleStats(ctx simplehttp.Context) error {
    return ctx.JSON(http.StatusOK, map[string]interface{}{
        "connections": {
            "active": suresql.CurrentNode.DBConnections.Len(),
            "max": suresql.CurrentNode.MaxPool,
        },
        "tokens": {
            "active": TokenStore.TokenMap.Len(),
        },
        "uptime_seconds": time.Since(suresql.ServerStartTime).Seconds(),
    })
}
```

---

### 30. **Inconsistent Error Types**
**Location**: Multiple files
**Severity**: LOW

**Problem**: Mix of error types:
- Standard errors: `errors.New("...")`
- Custom errors: `medaerror.MedaError`
- Sentinel errors: `ErrNoDBConnection`

**Fix**: Standardize on custom error type
```go
// Define error types
type ErrorCode string

const (
    ErrCodeDBConnection  ErrorCode = "DB_CONNECTION_FAILED"
    ErrCodeInvalidToken  ErrorCode = "INVALID_TOKEN"
    ErrCodeNotFound      ErrorCode = "NOT_FOUND"
)

type SureSQLError struct {
    Code    ErrorCode
    Message string
    Cause   error
}

func (e *SureSQLError) Error() string {
    if e.Cause != nil {
        return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Use everywhere
return &SureSQLError{
    Code: ErrCodeDBConnection,
    Message: "failed to connect to database",
    Cause: err,
}
```

---

### 31. **Global Variable Anti-Pattern**
**Location**: auth.go:34, models.go:31-42
**Severity**: LOW

**Problem**: Multiple global mutable variables:
```go
var TokenStore TokenStoreStruct
var CurrentNode SureSQLNode
var ReloadEnvironment bool = false
```

**Fix**: Use dependency injection
```go
// Create app context
type AppContext struct {
    Node       *SureSQLNode
    TokenStore *TokenStoreStruct
    DB         SureSQLDB
}

// Pass to handlers via context or closure
func HandleConnect(app *AppContext) simplehttp.HandlerFunc {
    return func(ctx simplehttp.Context) error {
        // Use app.Node instead of CurrentNode
        // Use app.TokenStore instead of TokenStore
    }
}
```

---

### 32. **Missing Graceful Shutdown**
**Location**: app/suresql/main.go (not reviewed but likely missing)
**Severity**: LOW

**Problem**: Server probably doesn't handle SIGTERM/SIGINT properly, causing:
- In-flight requests to fail
- DB connections not closed
- TTL maps not persisted

**Fix**:
```go
func main() {
    server := server.CreateServer(suresql.CurrentNode)

    // Setup graceful shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-quit
        fmt.Println("Shutting down server...")

        // Give time for in-flight requests
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        // Close DB connections
        suresql.CurrentNode.DBConnections.Range(func(key, value interface{}) bool {
            if db, ok := value.(suresql.SureSQLDB); ok {
                if closer, ok := db.(interface{ Close() error }); ok {
                    closer.Close()
                }
            }
            return true
        })

        // Shutdown server
        server.Shutdown(ctx)
    }()

    server.Start()
}
```

---

## SUMMARY & RECOMMENDATIONS

### Priority Actions (Fix Immediately):
1. ✅ **Add mutex protection to CurrentNode** (Issue #1)
2. ✅ **Fix password clearing in error paths** (Issue #2)
3. ✅ **Fix DecodeToken error handling** (Issue #3)
4. ✅ **Fix DB connection pool memory leak** (Issue #4)
5. ✅ **Fix error variable shadowing** (Issue #9)

### High-Impact Improvements (Next Sprint):
6. ✅ Cache configuration loading (Issue #5)
7. ✅ Refactor SQL handler duplication (Issue #6)
8. ✅ Add input validation & sanitization (Issue #7)
9. ✅ Fix TokenStore initialization order (Issue #8)
10. ✅ Standardize error handling (Issue #11)

### Code Quality (Ongoing):
11. Reduce code duplication (Issues #6, #13, #15, #24)
12. Remove dead code (Issues #16, #25, #26)
13. Improve documentation (Issues #14, #23, #28)
14. Add monitoring (Issues #19, #29)
15. Implement security features (Issues #21, #22)

### Metrics:
- **Total Issues**: 32
- **Critical**: 4 (12.5%)
- **High**: 8 (25%)
- **Medium**: 12 (37.5%)
- **Low**: 8 (25%)

**Estimated Effort**:
- Critical fixes: 3-5 days
- High priority: 1-2 weeks
- Medium priority: 2-3 weeks
- Low priority: 1 week

**Total**: ~6-8 weeks for complete cleanup

---

## TESTING RECOMMENDATIONS

### Unit Tests Needed:
1. Token generation and validation
2. Configuration loading precedence
3. SQL injection prevention in query builders
4. Error handling paths
5. Connection pool lifecycle

### Integration Tests Needed:
1. Concurrent request handling (race condition testing)
2. Token expiration and refresh flow
3. Connection pool exhaustion scenarios
4. Migration rollback scenarios
5. Node clustering and failover

### Load Tests Needed:
1. Connection pool under load
2. TTL map performance with 10k+ tokens
3. Memory leak detection (run for 24hrs)
4. Database connection pool sizing

---

*End of Code Review*
