package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/goutil/encryption"
	"github.com/medatechnology/goutil/object"
	"github.com/medatechnology/simplehttp"
)

const (
	DEFAULT_INTERNAL_API = "/suresql"
)

// UserTable struct representing a user from the database
type UserTable struct {
	ID        int       `json:"id,omitempty"           db:"id"`
	Username  string    `json:"username,omitempty"     db:"username"`
	Password  string    `json:"password,omitempty"     db:"password"` // hashed
	RoleName  string    `json:"role_name,omitempty"    db:"role_name"`
	CreatedAt time.Time `json:"created_at,omitempty"   db:"created_at"`
}

func (u UserTable) TableName() string {
	return "_users"
}

// TODO: add all the ACL tables here as well.

// UserUpdateRequest represents the data for updating a user
type UserUpdateRequest struct {
	Username    string `json:"username"`                // Required to identify the user
	NewUsername string `json:"new_username,omitempty"`  // Optional new username
	NewPassword string `json:"new_password,omitempty"`  // Optional new password
	NewRoleName string `json:"new_role_name,omitempty"` // Optional new role
}

// Add these functions to your RegisterRoutes function in handler.go
func RegisterInternalRoutes(server simplehttp.Server) {
	// Create an internal group with Basic Auth protection
	internalAPI := server.Group(DEFAULT_INTERNAL_API)
	internalAPI.Use(simplehttp.MiddlewareBasicAuth(
		suresql.CurrentNode.InternalConfig.Username,
		suresql.CurrentNode.InternalConfig.Password,
	))
	// fmt.Println("Using user:", suresql.CurrentNode.InternalConnection.Config.Username, " pass:", suresql.CurrentNode.InternalConnection.Config.Password)

	// Register internal routes
	internalAPI.GET("/iusers", HandleListUsers)
	internalAPI.POST("/iusers", HandleCreateUser)
	internalAPI.PUT("/iusers", HandleUpdateUser)
	// internalAPI.DELETE("/iusers/:username", HandleDeleteUser)
	internalAPI.DELETE("/iusers", HandleDeleteUser)
	internalAPI.GET("/schema", HandleGetSchema)
	internalAPI.GET("/dbms_status", HandleDBMSStatus)
}

// HandleListUsers retrieves all users from the system (or filtered by username)
func HandleListUsers(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "list_users", UserTable{}.TableName())

	// Get optional filter parameter
	usernameFilter := ctx.GetQueryParam("username")

	// Build condition for query
	var condition orm.Condition
	if usernameFilter != "" {
		// If username filter is provided
		condition = orm.Condition{
			Field:    "username",
			Operator: "LIKE",
			Value:    "%" + usernameFilter + "%",
		}
	}
	// Select fields to order by
	condition.OrderBy = []string{"username ASC"}

	// Execute query
	var users []UserTable
	records, err := suresql.CurrentNode.InternalConnection.SelectManyWithCondition(UserTable{}.TableName(), &condition)

	if err != nil {
		// Check if it's a "no rows" error, which isn't actually an error for listing
		if err == orm.ErrSQLNoRows {
			// Return empty array instead of error
			users = []UserTable{}
		} else {
			return state.SetError("Failed to list users", err, http.StatusInternalServerError).LogAndResponse("failed to list users", nil, true)
		}
	} else {
		// Convert records to UserTable objects (omitting password for security)
		for _, record := range records {
			user := object.MapToStructSlowDB[UserTable](record.Data)
			user.Password = "" // Remove password from response
			users = append(users, user)
		}
	}

	return state.SetSuccess(fmt.Sprintf("Users retrieved successfully: %d", len(users)), users).LogAndResponse(fmt.Sprintf("success count:%d", len(users)), "SelectWithManyCondition", true)
}

// HandleCreateUser creates a new user in the system
func HandleCreateUser(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "create_user", UserTable{}.TableName())

	// Parse request body
	var createReq UserTable
	if err := ctx.BindJSON(&createReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("failed to parse request body", nil, true)
	}

	// Validate required fields
	if createReq.Username == "" || createReq.Password == "" {
		return state.SetError("Username and password are required", nil, http.StatusBadRequest).LogAndResponse("missing username or password", nil, true)
	}

	// Validate user input format and length
	if err := suresql.ValidateUserFields(createReq.Username, createReq.Password, createReq.RoleName); err != nil {
		return state.SetError("Invalid user input", err, http.StatusBadRequest).LogAndResponse("user validation failed", err, true)
	}

	// Check if user already exists
	_, err := userNameExist(createReq.Username)
	if err == nil {
		// No error means user was found
		return state.SetError("User already exists", nil, http.StatusConflict).LogAndResponse("user already exists, cannot create", nil, true)
	}

	// Hash the password
	hashedPassword, err := encryption.HashPin(
		createReq.Password,
		suresql.CurrentNode.Config.APIKey,
		suresql.CurrentNode.Config.ClientID,
	)
	if err != nil {
		return state.SetError("Failed to hash password", err, http.StatusInternalServerError).LogAndResponse("failed to hash password", nil, true)
	}
	createReq.Password = hashedPassword
	createReq.CreatedAt = time.Now().UTC()

	// Create user record
	userRec, err := orm.TableStructToDBRecord(createReq)
	if err != nil {
		return state.SetError("Failed to create user record", err, http.StatusInternalServerError).LogAndResponse("failed to convert struct to record", nil, true)
	}
	// remove ID so when inserting, id will be auto-generated (default value) from the DB
	delete(userRec.Data, "id")

	// Insert into database
	res := suresql.CurrentNode.InternalConnection.InsertOneDBRecord(userRec, false)
	err = res.Error
	if err != nil {
		return state.SetError("Failed to create user", err, http.StatusInternalServerError).LogAndResponse("failed to insert db", nil, true)
	}

	return state.SetSuccess("Users created successfully", map[string]string{
		"id":       fmt.Sprintf("%d", userRec.Data["id"]),
		"username": createReq.Username,
		"role":     createReq.RoleName,
	}).LogAndResponse(fmt.Sprintf("user %s created", createReq.Username), "InsertOneTableStruct", true)

	// return returnResponse(ctx, "User created successfully", map[string]string{
	// 	"id":       fmt.Sprintf("%d", userRec.Data["id"]),
	// 	"username": createReq.Username,
	// 	"role":     createReq.RoleName,
	// })
}

// HandleUpdateUser updates an existing user
func HandleUpdateUser(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "update_user", UserTable{}.TableName())

	// Parse request body
	var updateReq UserUpdateRequest
	if err := ctx.BindJSON(&updateReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("failed to parse request body", nil, true)
	}

	// Validate required fields
	if updateReq.Username == "" {
		return state.SetError("Username is required", nil, http.StatusBadRequest).LogAndResponse("missing username field", nil, true)
	}

	// Verify at least one update field is provided
	if updateReq.NewUsername == "" && updateReq.NewPassword == "" && updateReq.NewRoleName == "" {
		return state.SetError("No update fields provided", nil, http.StatusBadRequest).LogAndResponse("no update fields provided", nil, true)
	}

	// Check if user exists
	user, err := userNameExist(updateReq.Username)
	if err != nil {
		return state.SetError("User not found", err, http.StatusNotFound).LogAndResponse("user not found", nil, true)
	}
	// remove the password first before using it for response
	user.Password = ""

	// Prepare update SQL parts
	var updateFields []string
	var updateValues []interface{}

	// Update username if provided
	if updateReq.NewUsername != "" && updateReq.NewUsername != updateReq.Username {
		// Check if new username already exists
		_, err := userNameExist(updateReq.NewUsername)
		if err == nil {
			// No error means user was found
			return state.SetError("New username already exist", err, http.StatusConflict).LogAndResponse("cannot update to new username, already exist", nil, true)
		}

		updateFields = append(updateFields, "username = ?")
		updateValues = append(updateValues, updateReq.NewUsername)
	}

	// Update password if provided
	if updateReq.NewPassword != "" {
		hashedPassword, err := encryption.HashPin(
			updateReq.NewPassword,
			suresql.CurrentNode.Config.APIKey,
			suresql.CurrentNode.Config.ClientID,
		)
		if err != nil {
			return state.SetError("Failed to hash password", err, http.StatusInternalServerError).LogAndResponse("failed to hash password", nil, true)
		}
		updateFields = append(updateFields, "password = ?")
		updateValues = append(updateValues, hashedPassword)
	}

	// Update role if provided
	if updateReq.NewRoleName != "" && updateReq.NewRoleName != user.RoleName {
		updateFields = append(updateFields, "role_name = ?")
		updateValues = append(updateValues, updateReq.NewRoleName)
	}

	// If no fields need updating, return success
	if len(updateFields) == 0 {
		return state.SetSuccess("No changes provided", nil).LogAndResponse("no changes", nil, false)
	}

	// Build and execute update SQL
	updateSQL := "UPDATE " + UserTable{}.TableName() + " SET " + strings.Join(updateFields, ", ") + " WHERE username = ?"
	updateValues = append(updateValues, updateReq.Username)

	paramSQL := orm.ParametereizedSQL{
		Query:  updateSQL,
		Values: updateValues,
	}

	result := suresql.CurrentNode.InternalConnection.ExecOneSQLParameterized(paramSQL)
	if result.Error != nil {
		return state.SetError("Failed to update user", result.Error, http.StatusInternalServerError).LogAndResponse("failed to update db", nil, true)
	}

	return state.SetSuccess("Users updated successfully", user).
		LogAndResponse(fmt.Sprintf("user %s fields: %s updated", updateReq.Username, strings.ReplaceAll(strings.Join(updateFields, ", "), " = ?", "")),
			"ExecOneSQLParameterized", true)
}

// HandleDeleteUser deletes a user from the system
func HandleDeleteUser(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "delete_user", UserTable{}.TableName())

	// Get username from URL parameter
	username := ctx.GetQueryParam("username")
	if username == "" {
		return state.SetError("Username is required", nil, http.StatusBadRequest).LogAndResponse("missing username field", nil, true)
	}

	// Check if user exists
	// TODO: change this based on ID (get the UserTable returned) to follow best practice
	_, err := userNameExist(username)
	if err != nil {
		return state.SetError("User "+username+" not found", err, http.StatusNotFound).LogAndResponse("user "+username+" not found", nil, true)
	}

	// Delete the user
	deleteSQL := "DELETE FROM " + UserTable{}.TableName() + " WHERE username = ?"

	paramSQL := orm.ParametereizedSQL{
		Query:  deleteSQL,
		Values: []interface{}{username},
	}

	result := suresql.CurrentNode.InternalConnection.ExecOneSQLParameterized(paramSQL)
	if result.Error != nil {
		return state.SetError("Failed to delete user", result.Error, http.StatusInternalServerError).LogAndResponse("failed to delete from db", nil, true)
	}

	return state.SetSuccess("Users deleted successfully", nil).LogAndResponse(fmt.Sprintf("user %s deleted successfully", username), "ExecOneSQLParameterized", true)
}

// HandleGetSchema only for internal
func HandleGetSchema(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "handle_schema", suresql.SchemaTable)

	if strings.Contains(ctx.GetPath(), "getschema") {
		return state.SetError("schema is not exposed to API", nil, http.StatusUnauthorized).LogAndResponse("schema is not exposed to API", nil, true)
	}
	result := suresql.CurrentNode.InternalConnection.GetSchema(false, false)

	return state.SetSuccess("Schema get successfully", result).LogAndResponse("schema get successfully (should be internal)", "GetSchema", true)
}

// HandleGetSchema only for internal
func HandleDBMSStatus(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "dbms_status", suresql.SchemaTable)

	if strings.Contains(ctx.GetPath(), "dbms_status") {
		result, err := suresql.GetStatusInternal(suresql.CurrentNode.InternalConnection, suresql.INTERNAL_MODE)
		if err != nil {
			return state.SetError("DBMS status returns error", err, http.StatusInternalServerError).LogAndResponse("DBMS status returns error", err, true)
		}
		return state.SetSuccess("Get DBMS status successfully", result).LogAndResponse("get status DBMS successfully (should be internal)", "Status", true)
	}

	return state.SetError("DBMS status is not exposed to API", nil, http.StatusUnauthorized).LogAndResponse("DBMS status is not exposed to API", nil, true)
}
