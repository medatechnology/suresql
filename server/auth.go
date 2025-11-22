package server

import (
	"fmt"
	"time"

	"github.com/medatechnology/suresql"

	orm "github.com/medatechnology/simpleorm"

	"github.com/medatechnology/goutil/encryption"
	"github.com/medatechnology/goutil/medaerror"
	"github.com/medatechnology/goutil/medattlmap"
	"github.com/medatechnology/goutil/object"
)

// Constant for auth related like token settings
const (
	TOKEN_STRING            = "token"
	TOKEN_LENGTH_MULTIPLIER = 3 // Controls token length/complexity
)

// Global variables
var (
	// Instead of Redis, we use ttlmap is lighter
	// TokenMap        *medattlmap.TTLMap // For access tokens
	// RefreshTokenMap *medattlmap.TTLMap // For refresh tokens
	TokenStore TokenStoreStruct
)

// Mini Redis like Key-Value storage based on MedaTTLMap
type TokenStoreStruct struct {
	TokenMap        *medattlmap.TTLMap // For access tokens
	RefreshTokenMap *medattlmap.TTLMap // For refresh tokens
}

// InitTokenMaps initializes the token maps with configured TTLs from the node
func InitTokenMaps(tokenExp, refreshExp, ttlTicker time.Duration) {
	// Use actual configuration from database/environment, not hardcoded defaults
	TokenStore = NewTokenStore(tokenExp, refreshExp, ttlTicker)
}

func NewTokenStore(exp, rexp, ttlTicker time.Duration) TokenStoreStruct {
	return TokenStoreStruct{
		TokenMap:        medattlmap.NewTTLMap(exp, ttlTicker),
		RefreshTokenMap: medattlmap.NewTTLMap(rexp, ttlTicker),
	}
}

func (t TokenStoreStruct) GetAll() (map[string]interface{}, map[string]interface{}) {
	return t.TokenMap.Map(), t.RefreshTokenMap.Map()
}

// Check if tokenExist, if it is, return the value of the TokenMap[token] - which is interface{} type
func (t TokenStoreStruct) SaveToken(token suresql.TokenTable) {
	t.TokenMap.Put(token.Token, 0, token)
	t.RefreshTokenMap.Put(token.Refresh, 0, token)
}

// Check if tokenExist, if it is, return the value of the TokenMap[token] - which is interface{} type
func (t TokenStoreStruct) TokenExist(token string) (*suresql.TokenTable, bool) {
	val, ok := t.TokenMap.Get(token)
	// fmt.Println("All TokenMap:", t.TokenMap.Map())
	if !ok {
		return nil, false
	}
	tok := val.(suresql.TokenTable)
	return &tok, true
}

// Check if tokenExist, if it is, return the value of the TokenMap[token] - which is interface{} type
func (t TokenStoreStruct) RefreshTokenExist(token string) (*suresql.TokenTable, bool) {
	val, ok := t.RefreshTokenMap.Get(token)
	if !ok {
		return nil, false
	}
	tok := val.(suresql.TokenTable)
	return &tok, true
}

// This read from default _user table which is internal suresql table for username
// NOTE: Password is NOT cleared in this function - caller must clear it after use
func userNameExist(username string) (UserTable, error) {
	// Find user in database
	condition := orm.Condition{
		Field:    "username",
		Operator: "=",
		Value:    username,
	}

	var user UserTable
	userRecord, err := suresql.CurrentNode.InternalConnection.SelectOneWithCondition(user.TableName(), &condition)
	if err != nil {
		return user, err
	}

	// Convert to User struct
	user = object.MapToStructSlowDB[UserTable](userRecord.Data)
	// Password is intentionally kept for passwordMatch() validation
	// Callers MUST clear user.Password immediately after authentication
	return user, nil
}

func passwordMatch(user UserTable, pass string) error {
	encr, err := encryption.HashPin(pass, suresql.CurrentNode.Config.APIKey, suresql.CurrentNode.Config.ClientID)
	if err != nil {
		return err
	}
	if user.Password == encr {
		return nil
	} else {
		return medaerror.NewString("password mismatch for user " + user.Username)
	}
}

func createNewTokenResponse(user UserTable) suresql.TokenTable {
	var token suresql.TokenTable
	// Generate tokens using NewRandomTokenIterate with TOKEN_LENGTH_MULTIPLIER
	token.Token = encryption.NewRandomTokenIterate(TOKEN_LENGTH_MULTIPLIER)
	token.Refresh = encryption.NewRandomTokenIterate(TOKEN_LENGTH_MULTIPLIER)
	token.UserID = fmt.Sprintf("%d", user.ID)
	token.UserName = user.Username
	token.TokenExpiresAt = time.Now().Add(suresql.DEFAULT_TOKEN_EXPIRES_MINUTES)
	token.RefreshExpiresAt = time.Now().Add(suresql.DEFAULT_REFRESH_EXPIRES_MINUTES)

	// Store tokens in TTL maps with appropriate expiration times
	TokenStore.SaveToken(token)

	// Record token creation metric
	suresql.Metrics.RecordTokenCreated()

	// Return tokens in response
	return token
}

