package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const randomBytes = 16

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

func randomSuffix() (string, error) {
	random := make([]byte, randomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}
