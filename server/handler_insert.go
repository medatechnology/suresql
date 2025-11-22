package server

import (
	"fmt"
	"net/http"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/simplehttp"
)

// HandleInsert processes record insertion requests
func HandleInsert(ctx simplehttp.Context) error {
	state := NewHandlerTokenState(ctx, "/insert/", "request")

	// Get username from context (set by TokenValidationFromTTL)
	if state.Token == nil {
		return state.SetError("Cannot retrieve token from context", nil, http.StatusUnauthorized).LogAndResponse("cannot retrieve token from context, should not happen because of middleware", nil, true)
	}

	// Parse request body
	var insertReq suresql.InsertRequest
	if err := ctx.BindJSON(&insertReq); err != nil {
		return state.SetError("Invalid request format", err, http.StatusBadRequest).LogAndResponse("Failed to parse request body", nil, true)
	}

	// Validate that records are provided
	numRecs := len(insertReq.Records)
	if numRecs == 0 {
		return state.SetError("No records provided", nil, http.StatusBadRequest).LogAndResponse("no records in request body", nil, true)
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

	// Execute the appropriate type of insert operation
	if numRecs == 1 {
		// Single record insert
		state.Label += "InsertOneDBRecord"

		// We need to pass by reference for the single record
		result := userDB.InsertOneDBRecord(insertReq.Records[0], insertReq.Queue)
		if result.Error != nil {
			return state.SetError("Failed to insert record", result.Error, http.StatusInternalServerError).LogAndResponse("failed to insert record", insertReq, true)
		}
		response.Results = append(response.Results, result)
		response.RowsAffected = numRecs
	} else if insertReq.SameTable {
		// Multiple records for the same table
		state.Label += "InsertManyDBRecordsSameTable"

		results, err := userDB.InsertManyDBRecordsSameTable(insertReq.Records, insertReq.Queue)
		if err != nil {
			return state.SetError("Failed to insert multiple records of same table", err, http.StatusInternalServerError).LogAndResponse("failed to insert multiple multiple records of same table", insertReq, true)
		}
		response.Results = results
		response.RowsAffected = numRecs
	} else {
		// Multiple records for different tables
		state.Label += "InsertManyDBRecords"

		results, err := userDB.InsertManyDBRecords(insertReq.Records, insertReq.Queue)
		if err != nil {
			return state.SetError("Failed to insert multiple records", err, http.StatusInternalServerError).LogAndResponse("failed to insert multiple multiple records", insertReq, true)
		}
		response.Results = results
		response.RowsAffected = len(results)
	}

	// Calculate total execution time
	response.ExecutionTime = state.SaveStopTimer()
	return state.SetSuccess(fmt.Sprintf("Successfully inserted %d records", response.RowsAffected), response).LogAndResponse("insert successfully", response, true)
}

