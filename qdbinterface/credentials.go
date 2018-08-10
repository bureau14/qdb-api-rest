package qdbinterface

import (
	"github.com/bureau14/qdb-api-go"
)

// Credentials : qdb user json credentials
type Credentials struct {
	Username  string `json:"username"`
	SecretKey string `json:"secret_key"`
}

// CredentialsFromFile : get user credentials from a file
func CredentialsFromFile(filename string) (string, string, error) {
	return qdb.UserCredentialFromFile(filename)
}
