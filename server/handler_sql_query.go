package server

import (
	"net/http"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/simplehttp"
)

// Note: Route registration is now handled in the main RegisterRoutes function

// HandleSQLExecution processes SQL execution requests
// It is protected by both API Key (from AuthMiddleware) and Token (from TokenValidationMiddleware)
func HandleSQLQuery(ctx simplehttp.Context) error {
	state := NewHandlerTokenState(ctx, "/querysql/", "request")
	// Get username from context (set by TokenValidationFromTTL)
	if state.Token == nil {
		return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized).LogAndResponse("cannot retrieve token from context, should not happen because of middleware", nil, true)
	}

	// Parse request body
	var queryReqSQL suresql.SQLRequest
	if err := ctx.BindJSON(&queryReqSQL); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("Failed to parse request body", nil, true)
	}

	// Validate that at least one of Statements or ParamSQL is provided
	if len(queryReqSQL.Statements) == 0 && len(queryReqSQL.ParamSQL) == 0 {
		return state.SetError("No SQL statements provided", nil, http.StatusBadRequest).LogAndResponse("no sql statement in request body", nil, true)
	}

	// Find the user's database connection from TTL map
	userDB, err := suresql.CurrentNode.GetDBConnectionByToken(state.Token.Token)
	if err != nil {
		return state.SetError("Cannot get DB connection", err, http.StatusInternalServerError).LogAndResponse("cannot get DB connection, maybe disconnected", nil, true)
	}

	// Prepare response
	var reponseMulti suresql.QueryResponseSQL

	// Execute the appropriate type of SQL statements
	if len(queryReqSQL.Statements) > 0 {
		// Raw SQL statements
		if len(queryReqSQL.Statements) == 1 {
			if queryReqSQL.SingleRow {
				// Single raw SQL statement
				state.Label += "SelectOnlyOneSQL"
				// result := userDB.SelectOneSQL(sqlReq.Statements[0])
				record, err := userDB.SelectOnlyOneSQL(queryReqSQL.Statements[0])
				if err != nil {
					if err == orm.ErrSQLNoRows {
						// No results found - return empty result
						state.LogMessage = "executed with no results"
					} else {
						return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
					}
				} else {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       orm.DBRecords{record},
						Count:         1,
						ExecutionTime: state.SaveStopTimer(),
					}
					reponseMulti = append(reponseMulti, response)
					state.LogMessage = "executed successfully"
				}
			} else {
				// Single raw SQL statement
				state.Label += "SelectOneSQL"
				// result := userDB.SelectOneSQL(sqlReq.Statements[0])
				records, err := userDB.SelectOneSQL(queryReqSQL.Statements[0])
				if err != nil {
					if err == orm.ErrSQLNoRows {
						// No results found - return empty result
						state.LogMessage = "executed with no results"
					} else {
						return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
					}
				} else {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       records,
						Count:         len(records),
						ExecutionTime: state.SaveStopTimer(),
					}
					reponseMulti = append(reponseMulti, response)
					state.LogMessage = "executed successfully"
				}
			}
		} else {
			// Multiple raw SQL statements
			state.Label += "SelectManySQL"
			records, err := userDB.SelectManySQL(queryReqSQL.Statements)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
				}
			} else {
				timing := state.SaveStopTimer()
				for _, rs := range records {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       rs,
						Count:         len(rs),
						ExecutionTime: timing,
					}
					reponseMulti = append(reponseMulti, response)
				}
				state.LogMessage = "executed successfully"
			}
		}
	} else if len(queryReqSQL.ParamSQL) > 0 {
		// Parameterized SQL statements
		if len(queryReqSQL.ParamSQL) == 1 {
			if queryReqSQL.SingleRow {
				// Single raw SQL statement
				state.Label += "SelectOnlyOneSQLParameterized"
				// result := userDB.SelectOneSQL(sqlReq.Statements[0])
				record, err := userDB.SelectOnlyOneSQLParameterized(queryReqSQL.ParamSQL[0])
				if err != nil {
					if err == orm.ErrSQLNoRows {
						// No results found - return empty result
						state.LogMessage = "executed with no results"
					} else {
						return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
					}
				} else {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       orm.DBRecords{record},
						Count:         1,
						ExecutionTime: state.SaveStopTimer(),
					}
					reponseMulti = append(reponseMulti, response)
					state.LogMessage = "executed successfully"
				}
			} else {
				// Single parameterized SQL statement
				state.Label += "SelectOneSQLParameterized"
				records, err := userDB.SelectOneSQLParameterized(queryReqSQL.ParamSQL[0])
				if err != nil {
					if err == orm.ErrSQLNoRows {
						// No results found - return empty result
						state.LogMessage = "executed with no results"
					} else {
						return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
					}
				} else {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       records,
						Count:         len(records),
						ExecutionTime: state.SaveStopTimer(),
					}
					reponseMulti = append(reponseMulti, response)
					state.LogMessage = "executed successfully"
				}
			}
		} else {
			// Multiple parameterized SQL statements
			state.Label += "SelectManySQLParameterized"
			records, err := userDB.SelectManySQLParameterized(queryReqSQL.ParamSQL)
			if err != nil {
				if err == orm.ErrSQLNoRows {
					// No results found - return empty result
					state.LogMessage = "executed with no results"
				} else {
					return state.SetError("Failed to execute query", err, http.StatusInternalServerError).LogAndResponse("failed to execute "+state.Label, queryReqSQL, true)
				}
			} else {
				timing := state.SaveStopTimer()
				for _, rs := range records {
					// Add single record to response
					response := suresql.QueryResponse{
						Records:       rs,
						Count:         len(rs),
						ExecutionTime: timing,
					}
					reponseMulti = append(reponseMulti, response)
				}
				state.LogMessage = "executed successfully"
			}
		}
	}

	// Calculate total execution time
	return state.SetSuccess("SQL executed successfully", reponseMulti).LogAndResponse("raw sql query executed successfully", reponseMulti, true)
}
