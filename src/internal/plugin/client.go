//go:build windows

package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

// rpcRequest is sent over stdin.
type rpcRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// rpcResponse is received over stdout.
type rpcResponse struct {
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	Result map[string]any `json:"result"`
	Error  string         `json:"error"`
}

type pendingCall struct {
	ch  chan rpcResponse
	err error
}

// Client makes JSON-RPC calls to a running plugin.
type Client struct {
	host    *Host
	mu      sync.Mutex
	pending map[string]*pendingCall
}

// NewClient creates an RPC client for the given host.
func NewClient(host *Host) *Client {
	return &Client{
		host:    host,
		pending: make(map[string]*pendingCall),
	}
}

// Call sends a JSON-RPC request and waits for the matching response.
func (c *Client) Call(pluginName, method string, params map[string]any, timeout time.Duration) (map[string]any, error) {
	stdin, err := c.host.GetStdin(pluginName)
	if err != nil {
		return nil, err
	}
	// Verify stdout pipe is available (reader goroutine handles actual consumption)
	if _, err := c.host.GetStdout(pluginName); err != nil {
		return nil, err
	}

	id := uuid.New().String()
	req := rpcRequest{ID: id, Method: method, Params: params}

	pc := &pendingCall{ch: make(chan rpcResponse, 1)}
	c.mu.Lock()
	c.pending[id] = pc
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// Serialize and send
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	data = append(data, '\n')

	// Write to stdin (need to lock to avoid interleaved writes)
	c.mu.Lock()
	_, err = stdin.Write(data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// Read response — we must also handle responses for OTHER pending calls
	// and route them to the correct channel.
	timer := time.AfterFunc(timeout, func() {
		pc.ch <- rpcResponse{OK: false, Error: "请求超时"}
	})

	var resp rpcResponse
	select {
	case resp = <-pc.ch:
		timer.Stop()
	case <-time.After(timeout + 100*time.Millisecond):
		return nil, fmt.Errorf("请求超时: %s", method)
	}

	if !resp.OK {
		if resp.Error != "" {
			return nil, fmt.Errorf("插件错误: %s", resp.Error)
		}
		return nil, fmt.Errorf("插件返回失败: %s", method)
	}
	return resp.Result, nil
}

// StartReader launches a background goroutine that continuously reads stdout
// and routes responses to the appropriate pending call channels.
func (c *Client) StartReader(pluginName string) {
	stdout, err := c.host.GetStdout(pluginName)
	if err != nil {
		return
	}
	go func() {
		for stdout.Scan() {
			line := stdout.Bytes()
			var resp rpcResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue // skip non-JSON lines (e.g. startup logs)
			}
			c.mu.Lock()
			pc, ok := c.pending[resp.ID]
			c.mu.Unlock()
			if ok {
				pc.ch <- resp
			}
		}
		// stdout closed — plugin crashed
	}()
}

// Health checks if a plugin is responsive.
func (c *Client) Health(pluginName string) error {
	_, err := c.Call(pluginName, "health", nil, 5*time.Second)
	return err
}

// Info returns the plugin's self-reported metadata.
func (c *Client) Info(pluginName string) (map[string]any, error) {
	return c.Call(pluginName, "info", nil, 5*time.Second)
}

// scannerReader adapts a bufio.Scanner to io.Reader for line-level access.
type scannerReader struct {
	s *bufio.Scanner
}

func (r *scannerReader) Read(p []byte) (int, error) {
	if !r.s.Scan() {
		return 0, io.EOF
	}
	n := copy(p, r.s.Bytes())
	return n, nil
}
