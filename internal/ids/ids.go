package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	randomBytes     = 16
	randomHexLength = randomBytes * 2
)

func DatabaseID() (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("generate database ID: %w", err)
	}
	return "database_" + suffix, nil
}

func DatabaseInternalName() (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("generate database internal name: %w", err)
	}
	return "mb_db_" + suffix, nil
}

func RoleInternalName() (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("generate role internal name: %w", err)
	}
	return "mb_role_" + suffix, nil
}

func ValidDatabaseID(value string) bool {
	return validPrefixedHex(value, "database_")
}

func ValidDatabaseInternalName(value string) bool {
	return validPrefixedHex(value, "mb_db_")
}

func ValidRoleInternalName(value string) bool {
	return validPrefixedHex(value, "mb_role_")
}

func randomSuffix() (string, error) {
	random := make([]byte, randomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func validPrefixedHex(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+randomHexLength {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
