package suresql

import (
	"regexp"
	"strings"

	"github.com/medatechnology/goutil/medaerror"
	orm "github.com/medatechnology/simpleorm"
)

// Input validation constants
const (
	MaxUsernameLength  = 50
	MaxPasswordLength  = 100
	MaxTableNameLength = 64
	MaxRoleNameLength  = 50
)

// Regular expressions for validation
var (
	// Alphanumeric, underscore, dot, hyphen only
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	// Table names: must start with letter or underscore, then alphanumeric or underscore
	tableNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	// Role names: alphanumeric, space, underscore, hyphen
	roleNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_ -]+$`)
)

// ValidateTableName validates table names to prevent SQL injection
// - Only allows alphanumeric characters and underscores
// - Must start with a letter or underscore
// - Cannot be longer than MaxTableNameLength
// - Blocks access to internal tables (starting with _) unless allowInternal is true
func ValidateTableName(name string, allowInternal bool) error {
	if name == "" {
		return medaerror.NewString("table name cannot be empty")
	}

	if len(name) > MaxTableNameLength {
		return medaerror.Errorf("table name exceeds maximum length of %d characters", MaxTableNameLength)
	}

	// Check format
	if !tableNameRegex.MatchString(name) {
		return medaerror.NewString("invalid table name format: must start with letter/underscore and contain only alphanumeric characters and underscores")
	}

	// Prevent access to internal tables unless explicitly allowed
	if !allowInternal && strings.HasPrefix(name, "_") {
		return medaerror.NewString("access to internal tables is not allowed")
	}

	return nil
}

// ValidateUsername validates usernames for creation and authentication
// - Must be 1-MaxUsernameLength characters
// - Allows alphanumeric, underscore, dot, hyphen
func ValidateUsername(username string) error {
	if username == "" {
		return medaerror.NewString("username cannot be empty")
	}

	if len(username) > MaxUsernameLength {
		return medaerror.Errorf("username must not exceed %d characters", MaxUsernameLength)
	}

	// Check format
	if !usernameRegex.MatchString(username) {
		return medaerror.NewString("username contains invalid characters (only alphanumeric, underscore, dot, hyphen allowed)")
	}

	return nil
}

// ValidatePassword validates password requirements
// - Must be 1-MaxPasswordLength characters
// - Add additional complexity requirements if needed
func ValidatePassword(password string) error {
	if password == "" {
		return medaerror.NewString("password cannot be empty")
	}

	if len(password) > MaxPasswordLength {
		return medaerror.Errorf("password must not exceed %d characters", MaxPasswordLength)
	}

	// Optional: Add password complexity requirements
	// if len(password) < 8 {
	// 	return medaerror.NewString("password must be at least 8 characters")
	// }

	return nil
}

// ValidateRoleName validates role names
func ValidateRoleName(roleName string) error {
	if roleName == "" {
		return medaerror.NewString("role name cannot be empty")
	}

	if len(roleName) > MaxRoleNameLength {
		return medaerror.Errorf("role name must not exceed %d characters", MaxRoleNameLength)
	}

	if !roleNameRegex.MatchString(roleName) {
		return medaerror.NewString("role name contains invalid characters")
	}

	return nil
}

// ValidateUserFields validates user fields (username, password, role)
func ValidateUserFields(username, password, roleName string) error {
	if err := ValidateUsername(username); err != nil {
		return medaerror.Errorf("invalid username: %v", err)
	}

	if password != "" { // Password might be empty for updates that don't change password
		if err := ValidatePassword(password); err != nil {
			return medaerror.Errorf("invalid password: %v", err)
		}
	}

	if roleName != "" {
		if err := ValidateRoleName(roleName); err != nil {
			return medaerror.Errorf("invalid role: %v", err)
		}
	}

	return nil
}

// IsNoRowsError checks if an error is the "no rows" error
// This is a standard pattern in SureSQL: no rows is not treated as an error
// but as a successful query with empty results
func IsNoRowsError(err error) bool {
	if err == nil {
		return false
	}
	// Check against ORM's standard error
	return err == orm.ErrSQLNoRows ||
	       err.Error() == "sql: no rows in result set" ||
	       err.Error() == "no rows in result set"
}
