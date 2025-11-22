package server

import (
	"fmt"
	"net/http"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/simplehttp"
)

// Note: Route registration is now handled in the main RegisterRoutes function

// HandleSQLExecution processes SQL execution requests
// It is protected by both API Key (from AuthMiddleware) and Token (from TokenValidationMiddleware)
func HandleSQLExecution(ctx simplehttp.Context) error {
	state := NewHandlerTokenState(ctx, "/sql/", "request")

	// Get username from context (set by TokenValidationFromTTL)
	if state.Token == nil {
		return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized).LogAndResponse("cannot retrieve token from context, should not happen because of middleware", nil, true)
	}

	// Parse request body
	var sqlReq suresql.SQLRequest
	if err := ctx.BindJSON(&sqlReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("Failed to parse request body", nil, true)
	}

	// Validate that at least one of Statements or ParamSQL is provided
	if len(sqlReq.Statements) == 0 && len(sqlReq.ParamSQL) == 0 {
		return state.SetError("No SQL statements provided", nil, http.StatusBadRequest).LogAndResponse("no sql statement in request body", nil, true)
	}

	// Find the user's database connection from TTL map
	userDB, err := suresql.CurrentNode.GetDBConnectionByToken(state.Token.Token)
	if err != nil {
		return state.SetError("Cannot get DB connection", err, http.StatusInternalServerError).LogAndResponse("cannot get DB connection, maybe disconnected", nil, true)
	}

	// Prepare response
	response := suresql.SQLResponse{
		Results:       []orm.BasicSQLResult{},
		ExecutionTime: 0,
		RowsAffected:  0,
	}

	// var executionType string
	// var err error

	// Execute the appropriate type of SQL statements
	if len(sqlReq.Statements) > 0 {
		// Raw SQL statements
		if len(sqlReq.Statements) == 1 {
			// Single raw SQL statement
			state.Label += "ExecOneSQL"
			result := userDB.ExecOneSQL(sqlReq.Statements[0])
			response.Results = append(response.Results, result)

			if result.Error != nil {
				return state.SetError("Failed to execute SQL statement", result.Error, http.StatusInternalServerError).LogAndResponse("failed to execute sql statement", sqlReq.Statements, true)
			}
			response.RowsAffected += result.RowsAffected
		} else {
			// Multiple raw SQL statements
			state.Label += "ExecManySQL"
			results, err := userDB.ExecManySQL(sqlReq.Statements)
			if err != nil {
				return state.SetError("Failed to execute multiple SQL statements", err, http.StatusInternalServerError).LogAndResponse("failed to execute multiple sql statements", sqlReq.Statements, true)
			}
			response.Results = results
			for _, result := range results {
				response.RowsAffected += result.RowsAffected // sum all rowsAffected into final response
			}
		}
	} else if len(sqlReq.ParamSQL) > 0 {
		// Parameterized SQL statements
		if len(sqlReq.ParamSQL) == 1 {
			// Single parameterized SQL statement
			state.Label += "ExecOneSQLParameterized"
			result := userDB.ExecOneSQLParameterized(sqlReq.ParamSQL[0])
			response.Results = append(response.Results, result)

			if result.Error != nil {
				return state.SetError("Failed to execute parameterized SQL statement", result.Error, http.StatusInternalServerError).LogAndResponse("failed to execute parameterized sql statement", sqlReq.Statements, true)
			}
			response.RowsAffected += result.RowsAffected
		} else {
			// Multiple parameterized SQL statements
			state.Label += "ExecManySQLParameterized"
			results, err := userDB.ExecManySQLParameterized(sqlReq.ParamSQL)
			if err != nil {
				return state.SetError("Failed to execute multiple parameterized SQL statement", err, http.StatusInternalServerError).LogAndResponse("failed to execute multiple parameterized sql statement", summarizeSQLForLog(sqlReq), true)
			}
			response.Results = results
			for _, result := range results {
				response.RowsAffected += result.RowsAffected
			}
		}
	}

	// Calculate total execution time
	response.ExecutionTime = state.SaveStopTimer()
	return state.SetSuccess("SQL executed successfully", response).LogAndResponse("raw sql executed successfully", response, true)
}

// Helper function to create a summary of the SQL statements for logging
func summarizeSQLForLog(req suresql.SQLRequest) string {
	if !LOG_RAW_QUERY {
		return ""
	}
	if len(req.Statements) > 0 {
		if len(req.Statements) == 1 {
			return req.Statements[0]
		}
		return "Multiple SQL statements: " + req.Statements[0] + "... (" + fmt.Sprintf("%d", len(req.Statements)) + " total)"
	} else if len(req.ParamSQL) > 0 {
		if len(req.ParamSQL) == 1 {
			return req.ParamSQL[0].Query
		}
		return "Multiple parameterized SQL statements: " + req.ParamSQL[0].Query + "... (" + fmt.Sprintf("%d", len(req.ParamSQL)) + " total)"
	}
	return ""
}
