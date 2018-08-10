package qdbinterface

import (
	"time"

	qdb "github.com/bureau14/qdb-api-go"
)

// CreateHandle : creates a handle with config values
func CreateHandle(user, secret, uri, serverPublicKeyFile string) (*qdb.HandleType, error) {

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
		clusterKey, err := qdb.ClusterKeyFromFile(serverPublicKeyFile)
		if err != nil {
			return nil, err
		}
		err = handle.AddClusterPublicKey(clusterKey)
		err = handle.AddUserCredentials(user, secret)
	}

	// connect
	err = handle.Connect(uri)
	return &handle, err
}
