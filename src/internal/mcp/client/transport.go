package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"everevo/internal/httpclient"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Transport is the lowest-level communication channel for MCP JSON-RPC.
type Transport interface {
	// Send transmits a JSON-RPC request and returns the raw response bytes.
	// The caller is responsible for unmarshalling into the expected type.
	Send(method string, params any) (json.RawMessage, error)
	// Close shuts down the transport.
	Close() error
}

// ─── stdio Transport ────────────────────────────────────────────

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *strings.Builder
	mu     sync.Mutex
	// exited is closed when the child process exits
	exited chan struct{}
}

func newStdioTransport(command string, args []string, env []string) (*stdioTransport, error) {
	// Pre-flight: check if command exists on PATH — give a friendly message for npx
	if _, err := exec.LookPath(command); err != nil {
		if command == "npx" || command == "node" {
			return nil, fmt.Errorf("%s not found — Node.js is required for this MCP server.\nInstall from https://nodejs.org (LTS recommended)", command)
		}
		return nil, fmt.Errorf("%s not found in PATH: %w", command, err)
	}

	cmd := exec.Command(command, args...)
	cmd.Env = append(cmd.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	// Capture stderr into a buffer so we can include it in error messages
	var stderrBuf strings.Builder
	go func() { io.Copy(&stderrBuf, stderr) }()

	// Track process exit via channel
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	t := &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: &stderrBuf,
		exited: exited,
	}

	// Give the process a brief moment — if it exits immediately, it's a
	// launch failure (missing binary, npm package not installed, etc.).
	select {
	case <-exited:
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg == "" {
			errMsg = "(no stderr output)"
		}
		return nil, fmt.Errorf("process exited immediately after start\nstderr: %s", errMsg)
	case <-time.After(600 * time.Millisecond):
		// Process survived the startup window — looks good
	}

	return t, nil
}

func (t *stdioTransport) isExited() bool {
	select {
	case <-t.exited:
		return true
	default:
		return false
	}
}

func (t *stdioTransport) stderrTail() string {
	s := strings.TrimSpace(t.stderr.String())
	if s == "" {
		return ""
	}
	// Limit to last 500 chars — enough for the actionable part of an error
	if len(s) > 500 {
		s = "...(truncated)\n" + s[len(s)-500:]
	}
	return s
}

func (t *stdioTransport) Send(method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = b
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if _, err := fmt.Fprintf(t.stdin, "%s\n", payload); err != nil {
		tail := t.stderrTail()
		if tail != "" {
			return nil, fmt.Errorf("write: %w\nstderr: %s", err, tail)
		}
		return nil, fmt.Errorf("write: %w", err)
	}

	// Read response, skipping up to 3 non-JSON banner lines that cmd.exe /
	// npm sporadically emit on Windows before the first JSON-RPC response.
	var line string
	for attempt := 0; attempt < 4; attempt++ {
		raw, err := t.stdout.ReadString('\n')
		if err != nil {
			tail := t.stderrTail()
			if tail != "" {
				return nil, fmt.Errorf("read: %w\nstderr: %s", err, tail)
			}
			if t.isExited() {
				return nil, fmt.Errorf("read: %w (process exited)", err)
			}
			return nil, fmt.Errorf("read: %w", err)
		}
		line = strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if len(line) > 0 && line[0] == '{' {
			break // JSON start — valid response
		}
		log.Printf("[mcp stdio] banner skipped (attempt %d): %s", attempt+1, strings.TrimSpace(raw))
	}
	if len(line) == 0 || line[0] != '{' {
		return nil, fmt.Errorf("no valid JSON response after banner filter (got: %s)", line)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w\nraw: %s", err, line)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

func (t *stdioTransport) Close() error {
	t.stdin.Close()
	if t.cmd.Process != nil && !t.isExited() {
		t.cmd.Process.Kill()
	}
	// Wait already returns immediately if the process has exited
	t.cmd.Wait()
	return nil
}

// ─── HTTP Transport ─────────────────────────────────────────────

type httpTransport struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	nextID int64
}

func newHTTPTransport(url string) *httpTransport {
	return &httpTransport{
		url:    strings.TrimRight(url, "/"),
		client: httpclient.New(30 * time.Second),
	}
}

func (t *httpTransport) Send(method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextID++
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      t.nextID,
		Method:  method,
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = b
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", t.url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (t *httpTransport) Close() error {
	return nil
}
