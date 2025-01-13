package qdbinterface

import (
	"time"

	"github.com/bureau14/qdb-api-rest/models"

	"github.com/bureau14/qdb-api-go/v3"
)

// global structures, I know not pretty
// TODO(vianney) find a way to keep information
// from one call to another

// ClusterInformation : all cluster information
var ClusterInformation models.Cluster

// NodesInformation : all nodes information
var NodesInformation = make(map[string]models.Node)

// Do not overload the server with call
// really dumb implementation but will work for now
var lastUpdate = time.Unix(0, 0)
var updateInterval = time.Duration(5 * time.Second)
var lastError = error(nil)

func shouldUpdate() bool {
	now := time.Now()
	if lastUpdate.Add(updateInterval).After(now) {
		return false
	}
	lastUpdate = now
	return true
}

func resetInformation() {
	ClusterInformation.MemoryTotal = new(int64)
	ClusterInformation.MemoryUsed = new(int64)
	ClusterInformation.Nodes = []string{}
	ClusterInformation.Status = new(string)
}

// RetrieveInformation : retrieve all informations
func RetrieveInformation(handle qdb.HandleType) error {
	if !shouldUpdate() && lastError == nil {
		return nil
	}

	if ClusterInformation.Status == nil {
		ClusterInformation.Status = new(string)
	}

	stats, err := handle.Statistics()
	if err == nil {
		*ClusterInformation.Status = "stable"
	} else {
		*ClusterInformation.Status = "unstable"
	}

	memoryTotal := int64(0)
	memoryUsed := int64(0)

	ClusterInformation.Nodes = []string{}
	NodesInformation = make(map[string]models.Node)
	for _, stat := range stats {
		memoryTotal += stat.Memory.Physmem.Total
		memoryUsed += stat.Memory.Physmem.Used
		ClusterInformation.Nodes = append(ClusterInformation.Nodes, stat.NodeID)

		id := stat.NodeID
		node := models.Node{}
		node.ID = &id
		node.MemoryTotal = &stat.Memory.Physmem.Total
		node.MemoryUsed = &stat.Memory.Physmem.Used
		node.Os = &stat.OperatingSystem
		node.QuasardbVersion = &stat.EngineVersion
		NodesInformation[id] = node
	}
	ClusterInformation.MemoryTotal = &memoryTotal
	ClusterInformation.MemoryUsed = &memoryUsed
	return err
}
