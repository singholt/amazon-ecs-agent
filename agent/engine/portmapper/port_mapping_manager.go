package portmapper

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/aws/amazon-ecs-agent/agent/utils"

	"github.com/cihub/seelog"
)

type PortMappingManager interface {
	// GetHostPortRange returns a contiguous host port range, equal to the number of ports requested
	GetHostPortRange(int, string) (string, error)
	// GetStartHostPortRange returns the start host port of the ephemeral range
	GetStartHostPortRange() int
	// GetEndHostPortRange returns the end host port of the ephemeral range
	GetEndHostPortRange() int
	// GetLastAssignedHostPort returns the last host port agent assigned to a container
	GetLastAssignedHostPort() int
	// SetLastAssignedHostPort sets the last host port agent assigned to a container
	SetLastAssignedHostPort(int)
}

// portMappingManager implements the PortMappingManager interface
type portMappingManager struct {
	// startHostPortRange is the start of the ephemeral host port range defined on the host
	startHostPortRange int
	// endHostPortRange is the end of the ephemeral host port range defined on the host
	endHostPortRange int
	// lastAssignedHostPort is the last host port the agent assigned to a container
	lastAssignedHostPort int

	// lock is used for lastAssignedHostPort that is accessed and updated concurrently
	lock sync.RWMutex
}

// NewPortMappingManager creates a portMappingManager to track the ephemeral range and our last checked host port
func NewPortMappingManager() PortMappingManager {
	// get ephemeral port range, either default or if custom-defined on the host
	start, end, err := utils.GetEphemeralHostPortRange()
	if err != nil {
		seelog.Warnf("Unable to read the ephemeral range, err: %v, falling back to the default range: %v-%v",
			err, utils.DefaultHostPortRangeStart, utils.DefaultHostPortRangeEnd)
	}
	return &portMappingManager{
		startHostPortRange: start,
		endHostPortRange:   end,
	}
}

// GetHostPortRange returns a set of contiguous host ports, equal to the numberOfPorts requested
func (pm *portMappingManager) GetHostPortRange(numberOfPorts int, protocol string) (string, error) {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	// requestStart and requestEnd are the ones between which we want to find contiguous host ports
	requestStart := pm.GetStartHostPortRange()
	requestEnd := pm.GetEndHostPortRange()

	lastAssignedHostPort := pm.GetLastAssignedHostPort()
	if lastAssignedHostPort != 0 {
		// this implies that this is not the first time we're looking for contiguous host ports,
		// so we want to start looking after lastAssignedHostPort.
		requestStart = lastAssignedHostPort + 1
	}

	resultStartPort, resultEndPort, ok := utils.GetContiguousPorts(numberOfPorts, requestStart, requestEnd, protocol)
	if !ok {
		if lastAssignedHostPort != 0 {
			// we start from the beginning of the ephemeral range to the lastAssignedHostPort-1.
			// this ensures that we check for ports that may have been freed up by stopped containers.
			requestStart = pm.GetStartHostPortRange()
			requestEnd = lastAssignedHostPort - 1
			resultStartPort, resultEndPort, ok = utils.GetContiguousPorts(numberOfPorts, requestStart, requestEnd, protocol)
			if !ok {
				return "", fmt.Errorf("%v contiguous host ports unavailable", numberOfPorts)
			}
		} else {
			// this implies that we failed to get contiguous ports the very first time we tried
			return "", fmt.Errorf("%v contiguous host ports unavailable", numberOfPorts)
		}
	}

	// update the last assigned port in portMappingManager so that all requests following the current one,
	// iterate from that port onwards.
	pm.SetLastAssignedHostPort(resultEndPort)
	return strconv.Itoa(resultStartPort) + "-" + strconv.Itoa(resultEndPort), nil
}

// GetStartHostPortRange returns the start host port of the ephemeral range
func (pm *portMappingManager) GetStartHostPortRange() int {
	pm.lock.RLock()
	defer pm.lock.RUnlock()

	return pm.startHostPortRange
}

// GetEndHostPortRange returns the end host port of the ephemeral range
func (pm *portMappingManager) GetEndHostPortRange() int {
	pm.lock.RLock()
	defer pm.lock.RUnlock()

	return pm.endHostPortRange
}

// GetLastAssignedHostPort returns the last host port agent assigned to a container
func (pm *portMappingManager) GetLastAssignedHostPort() int {
	pm.lock.RLock()
	defer pm.lock.RUnlock()

	return pm.lastAssignedHostPort
}

// SetLastAssignedHostPort sets the last host port agent assigned to a container
func (pm *portMappingManager) SetLastAssignedHostPort(port int) {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	pm.lastAssignedHostPort = port
}
