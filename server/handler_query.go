package server

import (
	"net/http"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/simplehttp"
)

// In SureSQL because we are serving API, if there is DB error no-rows, usually we do not want to return API-error.
// So make sure the caller (front-end) check if the return is 0 rows then handle it as error or not.

// HandleQuery processes data query requests
func HandleQuery(ctx simplehttp.Context) error {

	// Get headers using the correct approach
	state := NewHandlerTokenState(ctx, "/query/", "request")

	// Get username from token (set by TokenValidationFromTTL)
	if state.Token == nil {
		return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized).LogAndResponse("cannot retrieve token from context, should not happen because of middleware", nil, true)
	}

	// Parse request body
	var queryReq suresql.QueryRequest
	if err := ctx.BindJSON(&queryReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("Failed to parse request body", nil, true)
	}

	// Validate that table name is provided
	if queryReq.Table == "" {
		return state.SetError("Table name is required", nil, http.StatusBadRequest).LogAndResponse("no table name in request body", nil, true)
	}

	// Validate table name format to prevent SQL injection
	if err := suresql.ValidateTableName(queryReq.Table, false); err != nil {
		return state.SetError("Invalid table name", err, http.StatusBadRequest).LogAndResponse("table name validation failed", err, true)
	}

	// Find the user's database connection from TTL map
	userDB, err := suresql.CurrentNode.GetDBConnectionByToken(state.Token.Token)
	if err != nil {
		return state.SetError("Cannot get DB connection", err, http.StatusInternalServerError).LogAndResponse("cannot get DB connection, maybe disconnected", nil, true)
	}

	// Prepare response
	response := suresql.QueryResponse{
		Records:       []orm.DBRecord{},
		ExecutionTime: 0,
		Count:         0,
	}

	// Check if we have a condition
	hasCondition := queryReq.Condition != nil && !isEmptyCondition(queryReq.Condition)

	// Use the appropriate query function based on SingleRow and Condition
	if queryReq.SingleRow {
		if hasCondition {
			// SelectOneWithCondition
			state.Label += "SelectOneWithCondition"
			record, err := userDB.SelectOneWithCondition(queryReq.Table, queryReq.Condition)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute SelectOneWithCondition", queryReq, true)
				}
			} else {
				// Add single record to response
				response.Records = append(response.Records, record)
				response.Count = 1
				state.LogMessage = "executed successfully"
			}
		} else {
			// SelectOne
			state.Label += "SelectOne"
			record, err := userDB.SelectOne(queryReq.Table)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result (but not error)
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute SelectOne", queryReq, true)
				}
			} else {
				// Add single record to response
				response.Records = append(response.Records, record)
				response.Count = 1
				state.LogMessage = "executed successfully"
			}
		}
	} else {
		if hasCondition {
			// SelectManyWithCondition
			state.Label += "SelectManyWithCondition"
			records, err := userDB.SelectManyWithCondition(queryReq.Table, queryReq.Condition)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute SelectManyWithCondition", queryReq, true)
				}
			} else {
				response.Records = records
				response.Count = len(records)
				state.LogMessage = "executed successfully"
			}
		} else {
			// SelectMany
			state.Label += "SelectMany"
			records, err := userDB.SelectMany(queryReq.Table)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute SelectMany", queryReq, true)
				}
			} else {
				response.Records = records
				response.Count = len(records)
				state.LogMessage = "executed successfully"
			}
		}
	}

	// Calculate total execution time
	response.ExecutionTime = state.SaveStopTimer()
	return state.SetSuccess("Query executed successfully", response).LogAndResponse("query executed successfully", response, true)
}

// Helper function to check if a condition is empty
func isEmptyCondition(c *orm.Condition) bool {
	return c.Field == "" && len(c.Nested) == 0 &&
		len(c.OrderBy) == 0 && len(c.GroupBy) == 0 &&
		c.Limit == 0 && c.Offset == 0
}
