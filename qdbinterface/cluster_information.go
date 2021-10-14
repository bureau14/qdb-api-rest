package qdbinterface

import (
	"time"

	"github.com/bureau14/qdb-api-rest/models"

	"bureau14/qdb-api-go"
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
	ClusterInformation.DiskTotal = new(int64)
	ClusterInformation.DiskUsed = new(int64)
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

	diskTotal := int64(0)
	diskUsed := int64(0)
	memoryTotal := int64(0)
	memoryUsed := int64(0)

	ClusterInformation.Nodes = []string{}
	NodesInformation = make(map[string]models.Node)
	for _, stat := range stats {
		diskUsedByNode := stat.Disk.BytesTotal - stat.Disk.BytesFree
		diskTotal += stat.Disk.BytesTotal
		diskUsed += diskUsedByNode
		memoryTotal += stat.Memory.Physmem.Total
		memoryUsed += stat.Memory.Physmem.Used
		ClusterInformation.Nodes = append(ClusterInformation.Nodes, stat.NodeID)

		id := stat.NodeID
		node := models.Node{}
		cpuUser := int64(stat.CPU.User)
		cpuSystem := int64(stat.CPU.System)
		cpuIdle := stat.CPU.Idle
		cpuUsed := cpuUser + cpuSystem
		cpuTotal := cpuUsed + cpuIdle
		node.CPUTotal = &cpuTotal
		node.CPUUsed = &cpuUsed
		node.DiskTotal = &stat.Disk.BytesTotal
		node.DiskUsed = &diskUsedByNode
		node.ID = &id
		node.MemoryTotal = &stat.Memory.Physmem.Total
		node.MemoryUsed = &stat.Memory.Physmem.Used
		node.Os = &stat.OperatingSystem
		node.QuasardbVersion = &stat.EngineVersion
		NodesInformation[id] = node
	}
	ClusterInformation.DiskTotal = &diskTotal
	ClusterInformation.DiskUsed = &diskUsed
	ClusterInformation.MemoryTotal = &memoryTotal
	ClusterInformation.MemoryUsed = &memoryUsed
	return err
}
