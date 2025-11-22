package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/medatechnology/suresql"

	utils "github.com/medatechnology/goutil"
	"github.com/medatechnology/goutil/medaerror"
	"github.com/medatechnology/goutil/metrics"
	"github.com/medatechnology/goutil/object"
	"github.com/medatechnology/goutil/simplelog"
	"github.com/medatechnology/simplehttp"
	"github.com/medatechnology/simplehttp/framework/fiber"
)

// Define constants for token expiration and generation
const (
	DEFAULT_HTTP_ENVIRONMENT = "./.env.suresql"
	LOG_RAW_QUERY            = false // TODO: If enabled, log queries instead of just results
)

// if DB settings is not there, get from environment. DB's settings table always wins
func CopySettingsFromSureSQL(cnode suresql.SureSQLNode, config *simplehttp.Config) {
	if cnode.Config.Host != "" {
		config.Hostname = cnode.Config.Host
	}
	if cnode.Config.Port != "" {
		config.Port = cnode.Config.Port
	}
	if cnode.Config.Label != "" {
		config.AppName = cnode.Config.Label
	}
	// CurrentNode.Settings.SSL = os.Getenv("SURESQL_SSL")
	config.SSLRedirect = cnode.Config.SSL
}

func CreateServer(cnode suresql.SureSQLNode) simplehttp.Server {
	simplelog.DEBUG_LEVEL = 1

	el := metrics.StartTimeIt("Loading http environment...", 0)
	// Reload will overwrite, so put the most procedence last
	utils.ReloadEnvEach("./.env.simplehttp", DEFAULT_HTTP_ENVIRONMENT)
	// below is optional because simplehttp will look for environment variables
	// that is specific to simplehttp. While we want to use SureSQL setting.
	config := simplehttp.LoadConfig()
	CopySettingsFromSureSQL(cnode, config)
	metrics.StopTimeItPrint(el, "Done")

	el = metrics.StartTimeIt("Creating http server...", 0)
	// server := echo.NewServer(config)
	server := fiber.NewServer(config)
	metrics.StopTimeItPrint(el, "Done")

	// Initialize token maps (Redis alternative) with actual DB config
	el = metrics.StartTimeIt("Initializing TTLMaps (Redis alternative) ...", 0)
	InitTokenMaps(cnode.Config.TokenExp, cnode.Config.RefreshExp, cnode.Config.TTLTicker)
	metrics.StopTimeItPrint(el, "Done")

	// Initialize connection manager and start cleanup routine
	el = metrics.StartTimeIt("Starting connection cleanup routine...", 0)
	suresql.InitConnectionManager()
	// Start cleanup with a background context
	go suresql.StartConnectionCleanup(context.Background())
	metrics.StopTimeItPrint(el, "Done")

	// Initialize and start alert monitoring
	el = metrics.StartTimeIt("Starting alert monitoring system...", 0)
	suresql.InitAlertManager()
	go suresql.StartAlerting(context.Background())
	metrics.StopTimeItPrint(el, "Done")

	el = metrics.StartTimeIt("Registring endpoints ...", 0)
	RegisterRoutes(server)
	metrics.StopTimeItPrint(el, "Done")

	// this is internal end-points like adding/removing users (used by SaaS)
	// IMPORTANT TODO: separate this into SaaS only, SureSQL cloud.
	el = metrics.StartTimeIt("Registring internal endpoints ...", 0)
	RegisterInternalRoutes(server)
	metrics.StopTimeItPrint(el, "Done")

	// Register monitoring and metrics endpoints
	el = metrics.StartTimeIt("Registring monitoring endpoints ...", 0)
	RegisterMonitoringRoutes(server)
	metrics.StopTimeItPrint(el, "Done")

	return server
}

// RegisterRoutes sets up all the routes for the SureSQL API
func RegisterRoutes(server simplehttp.Server) {
	CORSConfig := &simplehttp.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           24 * time.Hour,
	}

	// Register global middleware
	server.Use(
		simplehttp.MiddlewareRecover(),
		simplehttp.MiddlewareCORS(CORSConfig),
		simplehttp.MiddlewareHeaderParser(), // use ctx.Get(simplehttp.REQUEST_HEADER_PARSED_STRING).(*RequestHeader) to get header
		simplehttp.MiddlewareLogger(simplehttp.NewDefaultLogger()),
	)
	// server.UseMiddleware(LoggingMiddleware)

	db := server.Group("/db")
	// All API need API_KEY, later all queries need TOKEN
	db.Use(MiddlewareAPIKeyHeader())
	{
		db.POST("/connect", HandleConnect)
		db.POST("/refresh", HandleRefresh)
		db.GET("/pingpong", func(ctx simplehttp.Context) error {
			state := NewHandlerState(ctx, "", "/pingpong", "pingpong")
			return state.SetSuccess(suresql.PingPong(), nil).LogAndResponse("pingpong response", nil, true)
		})
	}

	api := db.Group("/api")
	api.Use(MiddlwareTokenCheck())
	{
		api.GET("/status", HandleDBStatus)
		api.GET("/getschema", HandleGetSchema) // this is actually not working, because it should be used only for SaaS
		api.POST("/sql", HandleSQLExecution)
		api.POST("/query", HandleQuery)
		api.POST("/querysql", HandleSQLQuery)
		api.POST("/insert", HandleInsert)
	}

}

// HandleConnect authenticates a user and returns tokens
func HandleConnect(ctx simplehttp.Context) error {
	// Set the state
	state := NewHandlerState(ctx, "", "/connect", UserTable{}.TableName())

	// Parse request body
	var connectReq UserTable // but only use username and password
	if err := ctx.BindJSON(&connectReq); err != nil {
		return state.SetError("invalid requesst format", err, 0).LogAndResponse("Failed to parse request body", nil, true)
	}
	state.User = connectReq.Username

	// Validate username format
	if err := suresql.ValidateUsername(connectReq.Username); err != nil {
		return state.SetError("Invalid username", err, http.StatusBadRequest).LogAndResponse("username validation failed", err, true)
	}

	// Check by username, NOTE: do we need to change this to user.ID instead?
	user, err := userNameExist(connectReq.Username)
	if err != nil {
		return state.SetError("Invalid credentials", nil, http.StatusUnauthorized).LogAndResponse("user not found", err, true)
	}

	// Verify password - in a real system, use proper password hashing
	if passwordMatch(user, connectReq.Password) != nil {
		return state.SetError("Invalid credentials", nil, http.StatusUnauthorized).
			LogAndResponse("password missmatch for user:"+connectReq.Username, err, true)
	}

	// SECURITY: Clear password immediately after authentication
	user.Password = ""

	// Copy the configuration from internal connection
	configCopy := suresql.CurrentNode.InternalConfig
	// configCopy.Username = user.Username
	state.User = user.Username

	// Create a new database connection with the copied config
	newDB, err := suresql.NewDatabase(configCopy)
	if err != nil {
		// Record failed authentication
		suresql.Metrics.RecordAuthentication(false)
		return state.SetError("Failed to create database connection", err, http.StatusInternalServerError).
			LogAndResponse("failed to create database connection", err, true)
	}

	// Generate tokens using NewRandomTokenIterate with TOKEN_LENGTH_MULTIPLIER
	tokenResponse := createNewTokenResponse(user)
	// state.OnlyLog("Generated tokens for user: "+user.Username, nil, true)

	// Add to connection pool if enabled
	if suresql.CurrentNode.IsPoolAvailable() {
		suresql.CurrentNode.DBConnections.Put(tokenResponse.Token, 0, newDB)
		// Record successful connection creation
		suresql.Metrics.RecordConnectionCreated()
		suresql.Metrics.RecordAuthentication(true)
		// state.OnlyLog(fmt.Sprintf("Added new connection to pool, current size: %d/%d", suresql.suresql.CurrentNode.DBConnections.Len(), suresql.CurrentNode.MaxPool), nil, true)
	} else {
		err := medaerror.NewString("db pool quota exceeded")
		// Record pool exhaustion
		suresql.Metrics.RecordPoolExhaustion()
		suresql.Metrics.RecordAuthentication(false)
		return state.SetError("Failed to create database connection, quota exceeded", err, http.StatusNotAcceptable).
			LogAndResponse("cannot create database connection, quota exceeded", nil, true)
	}

	// Return tokens in response
	return state.SetSuccess("Authentication successful", tokenResponse).
		LogAndResponse("user connected to db successfully", tokenResponse.Token, true)
	// return returnResponse(ctx, "Authentication successful", tokenResponse)
}

// HandleRefresh refreshes an existing token
func HandleRefresh(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/refresh", "cache/ttlmap")

	// Parse request body
	// var refreshReq RefreshRequest
	var refreshReq suresql.TokenTable
	if err := ctx.BindJSON(&refreshReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("Failed to parse request body", nil, true)
	}

	// Validate refresh token only from memory
	// username, ok := RefreshTokenMap.Get(refreshReq.RefreshToken)
	tokmap, ok := TokenStore.RefreshTokenExist(refreshReq.Refresh)
	if !ok {
		return state.SetError("Invalid or expired refresh token", nil, http.StatusUnauthorized).
			LogAndResponse("Invalid or expired refresh token:"+refreshReq.Refresh, nil, true)
	}

	state.User = tokmap.UserName

	// SECURITY FIX: Close old connection and create fresh one
	// Get old connection
	oldDB, err := suresql.CurrentNode.GetDBConnectionByToken(tokmap.Token)
	if err == nil {
		// Try to close if the connection supports it
		if closer, ok := interface{}(oldDB).(interface{ Close() error }); ok {
			if closeErr := closer.Close(); closeErr != nil {
				// Log but don't fail - connection might already be closed
				simplelog.LogErrorAny("refresh", closeErr, "failed to close old DB connection")
			} else {
				// Record successful connection close
				suresql.Metrics.RecordConnectionClosed()
			}
		}
	}

	// Remove old connection from pool
	suresql.CurrentNode.DBConnections.Delete(tokmap.Token)

	// Create new database connection
	configCopy := suresql.CurrentNode.GetInternalConfig()
	newDB, err := suresql.NewDatabase(configCopy)
	if err != nil {
		return state.SetError("Failed to create database connection", err, http.StatusInternalServerError).
			LogAndResponse("failed to create database connection on refresh", err, true)
	}

	// Generate new tokens
	tokenResponse := createNewTokenResponse(UserTable{Username: tokmap.UserName, ID: object.Int(tokmap.UserID, false)})

	// Add new connection to pool with new token
	if suresql.CurrentNode.IsPoolAvailable() {
		suresql.CurrentNode.DBConnections.Put(tokenResponse.Token, 0, newDB)
		// Record successful connection creation and refresh token usage
		suresql.Metrics.RecordConnectionCreated()
		suresql.Metrics.RecordRefreshTokenUsed()
	} else {
		// Record pool exhaustion
		suresql.Metrics.RecordPoolExhaustion()
		return state.SetError("Connection pool full", medaerror.NewString("pool quota exceeded"), http.StatusServiceUnavailable).
			LogAndResponse("cannot create new connection, pool full", nil, true)
	}

	// Remove old refresh token from store
	TokenStore.RefreshTokenMap.Delete(refreshReq.Refresh)

	return state.SetSuccess("Token refreshed successfully", tokenResponse).
		LogAndResponse("refreshed tokens for user: "+tokmap.UserName, nil, true)

}

// HandleDBStatus returns the current database status
func HandleDBStatus(ctx simplehttp.Context) error {
	state := NewHandlerTokenState(ctx, "db_status", "ttlmap/db")

	// Get username from context (set by TokenValidationFromTTL)
	if state.Token == nil {
		return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized).LogAndResponse("cannot retrieve token from context, should not happen because of middleware", nil, true)
	}

	// Find the user's database connection from TTL map
	userDB, err := suresql.CurrentNode.GetDBConnectionByToken(state.Token.Token)
	if err != nil {
		return state.SetError("Cannot get DB connection", err, http.StatusInternalServerError).LogAndResponse("cannot get DB connection, maybe disconnected", nil, true)
		// returnErrorResponse(ctx, http.StatusUnauthorized, "Cannot get DB connection", err)
	}

	// Get database status
	// TODO: at this moment the status from RQLite only get the leader and peers as string
	// -     this should change to getting the real status from rqlite, but go-rqlite is limited!
	status, err := suresql.GetStatusInternal(userDB, suresql.NODE_MODE)
	if err != nil {
		return state.SetError("Cannot get DB status", err, http.StatusInternalServerError).LogAndResponse("cannot get DB status", nil, true)
	}
	msg := "Status peers vs config mismatched"
	// TODO: check which one is valid, from the RQLIte status vs SureSQLNode.Config which we put to status
	if len(suresql.CurrentNode.Status.Peers) == len(status.Peers) {
		msg = "Status peers vs config matched"
	}

	// NOTE: should we return the uptime of the DBMS behind SureSQL or just the uptime of SureSQL service server instead?
	// Now we are returning the server uptime, not the DBMS. If want the DBMS then set this to: status.Uptime.
	suresql.CurrentNode.Status.Uptime = time.Since(suresql.ServerStartTime) // this is refreshed when Status handler is called

	// return state.SetSuccess(msg, suresql.CurrentNode.Status).LogAndResponse(fmt.Sprintf("user: %s, db status: %s", state.User, status), suresql.CurrentNode.Settings, true)
	// Decided not to log the data for success
	return state.SetSuccess(msg, suresql.CurrentNode.Status).LogAndResponse(fmt.Sprintf("client user: %s", state.User), nil, true)
	// return state.SetSuccess(msg, map[string]interface{}{
	// 	"status":       suresql.CurrentNode.Status,
	// 	"node_info":    suresql.CurrentNode.Settings,
	// 	"connected_as": state.User,
	// }).LogAndResponse(fmt.Sprintf("user: %s, db status: %s", state.User, status), suresql.CurrentNode.Settings, true)

	// Return database status
	// return returnResponse(ctx, "Database status retrieved", map[string]interface{}{
	// 	"status":       status,
	// 	"node_info":    suresql.CurrentNode.Settings,
	// 	"connected_as": token.UserName,
	// })
}
