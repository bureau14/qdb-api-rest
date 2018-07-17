package qdbinterface

import (
	"fmt"
	"time"

	"github.com/bureau14/qdb-rest-api/models"

	"github.com/bureau14/qdb-api-go"
)

// TODO(vianney) create a library in a separate folder for all of this

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
	ClusterInformation.DiskTotal = new(int64)
	ClusterInformation.DiskUsed = new(int64)
	ClusterInformation.MemoryTotal = new(int64)
	ClusterInformation.MemoryUsed = new(int64)
	ClusterInformation.Nodes = []string{}
	ClusterInformation.Status = new(string)
}

func RetrieveInformation() error {
	if !shouldUpdate() && lastError == nil {
		return nil
	}

	// Not sure if smart to re-do the connection everytime
	// We should test on every node we retrieved recently
	handle, err := qdb.SetupHandle("qdb://127.0.0.1:2830", time.Duration(60*time.Second))
	if err != nil {
		resetInformation()
		fmt.Println("error:", err)
		*ClusterInformation.Status = "unreachable"
		lastError = err
		return err
	}
	defer handle.Close()
	nodeEndpoints, err := handle.Cluster().Endpoints()
	if ClusterInformation.Status == nil {
		ClusterInformation.Status = new(string)
	}
	if err == nil {
		*ClusterInformation.Status = "stable"
	} else {
		*ClusterInformation.Status = "unstable"
	}

	var nodeStatus []qdb.NodeStatus
	for _, nodeEndpoint := range nodeEndpoints {
		status, err := handle.Node(nodeEndpoint.URI()).Status()
		if err != nil {
			lastError = err
			return err
		}
		nodeStatus = append(nodeStatus, status)
	}
	lastError = nil

	diskTotal := int64(0)
	diskUsed := int64(0)
	memoryTotal := int64(0)
	memoryUsed := int64(0)

	ClusterInformation.Nodes = []string{}
	NodesInformation = make(map[string]models.Node)
	for _, status := range nodeStatus {
		diskUsedByNode := (status.DiskUsage.Total - status.DiskUsage.Free)
		diskTotal += status.DiskUsage.Total
		diskUsed += diskUsedByNode
		memoryTotal += status.Memory.Physmem.Total
		memoryUsed += status.Memory.Physmem.Used
		ClusterInformation.Nodes = append(ClusterInformation.Nodes, status.Network.ListeningEndpoint)

		id := status.Network.ListeningEndpoint
		node := models.Node{}
		cpuTotal := int64(status.CPUTimes.System)
		cpuUsed := float64(status.CPUTimes.User)
		node.CPUTotal = &cpuTotal
		node.CPUUsed = &cpuUsed
		node.DiskTotal = &status.DiskUsage.Total
		node.DiskUsed = &diskUsedByNode
		node.ID = &id
		node.MemoryTotal = &status.Memory.Physmem.Total
		node.MemoryUsed = &status.Memory.Physmem.Used
		node.Os = &status.OperatingSystem
		node.QuasardbVersion = &status.EngineVersion
		NodesInformation[id] = node
	}
	ClusterInformation.DiskTotal = &diskTotal
	ClusterInformation.DiskUsed = &diskUsed
	ClusterInformation.MemoryTotal = &memoryTotal
	ClusterInformation.MemoryUsed = &memoryUsed
	return nil
}
