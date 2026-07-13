package zone

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

const (
	defaultMCPBase = 19800
	defaultA2ABase = 19801
)

// portRegistry maps zone name → allocated ports.
type portRegistry map[string]portAlloc

type portAlloc struct {
	MCP int `json:"mcp"`
	A2A int `json:"a2a"`
}

var portsMu sync.Mutex

// portRegistryPath returns the path to the shared port registry file.
func portRegistryPath() string {
	return filepath.Join(storage.DataDir(), "port_registry.json")
}

// loadPorts reads the port allocation registry from disk.
func loadPorts() (portRegistry, error) {
	data, err := os.ReadFile(portRegistryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return portRegistry{}, nil
		}
		return nil, err
	}
	var reg portRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return portRegistry{}, nil
	}
	return reg, nil
}

// savePorts writes the port allocation registry to disk.
func savePorts(reg portRegistry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return atomic.WriteFile(portRegistryPath(), data, 0644)
}

// Allocate assigns unique MCP and A2A ports for the named zone.
// The production zone gets the fixed defaults; experiments scan upward.
func Allocate(name string, isProd bool) (mcpPort, a2aPort int, err error) {
	portsMu.Lock()
	defer portsMu.Unlock()

	reg, err := loadPorts()
	if err != nil {
		return 0, 0, fmt.Errorf("load port registry: %w", err)
	}

	// If this zone already has an entry, return it (reuse on restart).
	if alloc, ok := reg[name]; ok {
		if isPortAvailable(alloc.MCP) && isPortAvailable(alloc.A2A) {
			return alloc.MCP, alloc.A2A, nil
		}
		// Ports are busy — reallocate.
		delete(reg, name)
	}

	var mcp, a2a int
	if isProd {
		mcp, a2a = defaultMCPBase, defaultA2ABase
		// If the defaults are taken, scan upward.
		for !isPortAvailable(mcp) || !isPortAvailable(a2a) {
			mcp += 2
			a2a += 2
		}
	} else {
		// Scan from above production defaults.
		mcp, a2a = defaultMCPBase+2, defaultA2ABase+2
		for !isPortAvailable(mcp) || !isPortAvailable(a2a) || isPortRegistered(reg, mcp, a2a) {
			mcp += 2
			a2a += 2
		}
	}

	reg[name] = portAlloc{MCP: mcp, A2A: a2a}
	if err := savePorts(reg); err != nil {
		return 0, 0, fmt.Errorf("save port registry: %w", err)
	}
	return mcp, a2a, nil
}

// Release frees the ports assigned to the named zone.
func Release(name string) error {
	portsMu.Lock()
	defer portsMu.Unlock()

	reg, err := loadPorts()
	if err != nil {
		return fmt.Errorf("load port registry: %w", err)
	}
	delete(reg, name)
	return savePorts(reg)
}

// isPortAvailable checks whether a TCP port is free to listen on.
func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// isPortRegistered checks whether a port pair is already assigned in the registry.
func isPortRegistered(reg portRegistry, mcp, a2a int) bool {
	for _, a := range reg {
		if a.MCP == mcp || a.A2A == a2a {
			return true
		}
	}
	return false
}
