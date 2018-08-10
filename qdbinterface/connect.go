package qdbinterface

import (
	"os"
	"time"

	qdb "github.com/bureau14/qdb-api-go"
)

// CreateHandle : creates a handle with config values
func CreateHandle(user, secret string) (*qdb.HandleType, error) {

	handle, err := qdb.NewHandle()
	if err != nil {
		return nil, err
	}

	// Set timeout
	err = handle.SetTimeout(time.Duration(12) * time.Hour)
	if err != nil {
		return nil, err
	}

	if user != "" && secret != "" {
		// Set encryption if enabled server side
		err = handle.SetEncryption(qdb.EncryptNone)

		// add security if enabled server side
		serverPublicKeyFile := os.Getenv("SERVER_PUBLIC_KEY_FILE")
		clusterKey, err := qdb.ClusterKeyFromFile(serverPublicKeyFile)
		if err != nil {
			return nil, err
		}
		err = handle.AddClusterPublicKey(clusterKey)
		err = handle.AddUserCredentials(user, secret)
	}

	// connect
	uri := os.Getenv("CLUSTER_URI")
	err = handle.Connect(uri)
	return &handle, err
}
