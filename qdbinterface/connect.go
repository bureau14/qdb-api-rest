package qdbinterface

import (
	"fmt"
	"log"
	"time"

	qdb "github.com/bureau14/qdb-api-go"
)

// CreateHandle : creates a handle with config values
func CreateHandle(user, secret, uri, clusterPublicKeyFile string) (*qdb.HandleType, error) {

	handle, err := qdb.NewHandle()
	if err != nil {
		return nil, err
	}

	// Set timeout
	err = handle.SetTimeout(time.Duration(12) * time.Hour)
	if err != nil {
		return nil, err
	}

	if user != "" && secret != "" && clusterPublicKeyFile != "" {
		// Set encryption if enabled server side
		err = handle.SetEncryption(qdb.EncryptNone)

		// add security if enabled server side
		clusterKey, err := qdb.ClusterKeyFromFile(clusterPublicKeyFile)
		if err != nil {
			return nil, fmt.Errorf("Could not retrieve cluster key from file:%s", clusterPublicKeyFile)
		}
		err = handle.AddClusterPublicKey(clusterKey)
		err = handle.AddUserCredentials(user, secret)
	} else if clusterPublicKeyFile == "" {
		log.Printf("Warning: cannot connect user %s , cluster is not secured.", user)
	}

	// connect
	err = handle.Connect(uri)
	return &handle, err
}
