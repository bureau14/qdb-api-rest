package qdbinterface

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"

	"github.com/bureau14/qdb-api-go/v3"
)

// DefaultPrivateKey is the default rsa key used in non-secure cluster mode
var DefaultPrivateKey = MustUnmarshalRSAKey([]byte(`-----BEGIN PRIVATE KEY-----
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
/tYUvKn0Mo+6QAcxqpQw2xbIbgnZuVE4
-----END PRIVATE KEY-----`))

// Credentials : qdb user json credentials
type Credentials struct {
	Username  string `json:"username"`
	SecretKey string `json:"secret_key"`
}

// CredentialsFromFile : get user credentials from a file
func CredentialsFromFile(filename string) (string, string, error) {
	return qdb.UserCredentialFromFile(filename)
}

// MustUnmarshalRSAKeyFromFile reads and parses an RSA private key from a file
// and panics on an error
func MustUnmarshalRSAKeyFromFile(filePath string) *rsa.PrivateKey {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		panic(err)
	}
	return MustUnmarshalRSAKey(data)
}

// MustUnmarshalRSAKey reads and parses an RSA private key from a file and
// panics on an error
func MustUnmarshalRSAKey(data []byte) *rsa.PrivateKey {
	block, _ := pem.Decode(data)
	if block == nil {
		panic("Failed to decode PEM data")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		panic(err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		panic("key is not of type *rsa.PrivateKey")
	}

	return rsaKey
}
