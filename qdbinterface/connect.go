package qdbinterface

import (
	"fmt"
	"log"
	"time"

	qdb ""github.com/bureau14/qdb-api-go/v3""
)

// CreateHandle : creates a handle with config values
func CreateHandle(user, secret, uri, clusterPublicKeyFile string, maxInBufferSize uint) (*qdb.HandleType, error) {

	handle, err := qdb.NewHandle()
	if err != nil {
		return nil, err
	}

	// Set timeout
	err = handle.SetTimeout(time.Duration(12) * time.Hour)
	if err != nil {
		return nil, err
	}

	// Set max_in_buffer_size
	err = handle.SetClientMaxInBufSize(maxInBufferSize)
	if err != nil {
		err = fmt.Errorf("Invalid max in buffer size: %d", maxInBufferSize)
		return nil, err
	}

	if user != "" && secret != "" {
		if clusterPublicKeyFile != "" {
			// Set encryption if enabled server side
			err = handle.SetEncryption(qdb.EncryptNone)

			// add security if enabled server side
			clusterKey, err := qdb.ClusterKeyFromFile(clusterPublicKeyFile)
			if err != nil {
				return nil, fmt.Errorf("Could not retrieve cluster key from file:%s", clusterPublicKeyFile)
			}
			err = handle.AddClusterPublicKey(clusterKey)
			err = handle.AddUserCredentials(user, secret)
		} else {
			log.Printf("Warning: cannot connect user %s , cluster is not secured.", user)
		}
	}

	// connect
	err = handle.Connect(uri)
	return &handle, err
}
