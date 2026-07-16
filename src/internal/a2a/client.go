package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client is an A2A JSON-RPC client for connecting to remote agents.
type Client struct {
	url        string
	secret     string
	httpClient *http.Client
	card       *AgentCard
}

// NewClient creates an A2A client for the given agent URL.
// url should be the base URL, e.g. "http://127.0.0.1:19801".
func NewClient(url, secret string) *Client {
	return &Client{
		url:        strings.TrimRight(url, "/"),
		secret:     secret,
		httpClient: &http.Client{}, // no global timeout — each call bounds itself via ctx
	}
}

// Connect fetches the remote agent's AgentCard to validate the connection.
func (c *Client) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cardURL := c.url + "/.well-known/agent-card.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return fmt.Errorf("a2a client: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("a2a client: GET agent-card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("a2a client: agent-card returned %d: %s", resp.StatusCode, string(body))
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return fmt.Errorf("a2a client: decode agent-card: %w", err)
	}

	c.card = &card
	// Anti-forgery: if the card advertises a URL, it must be on the same host
	// we just connected to (a peer must not vouch for a different origin).
	if card.URL != "" {
		if adv, err := url.Parse(card.URL); err == nil {
			if conn, err := url.Parse(c.url); err == nil && adv.Host != "" && adv.Host != conn.Host {
				return fmt.Errorf("a2a client: agent-card host %q != connection host %q", adv.Host, conn.Host)
			}
		}
	}
	return nil
}

// GetCard returns the fetched agent card (nil if not connected).
func (c *Client) GetCard() *AgentCard {
	return c.card
}

// Ping checks if the remote agent is reachable.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cardURL := c.url + "/.well-known/agent-card.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// SendTask sends a task to the remote agent and waits for completion.
func (c *Client) SendTask(ctx context.Context, message Message) (*Task, error) {
	// Server-side executor may run up to 120s; allow headroom so the client
	// does not time out first and leave the task orphaned server-side.
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	taskID := uuid.New().String()
	params := SendParams{
		ID:      taskID,
		Message: message,
	}

	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"1"`),
		Method:  MethodSend,
		Params:  mustMarshal(params),
	}

	task, err := c.call(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// GetTask retrieves a task by ID from the remote agent.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := map[string]string{"id": taskID}
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"1"`),
		Method:  MethodGet,
		Params:  mustMarshal(params),
	}

	task, err := c.call(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// CancelTask cancels a task on the remote agent.
func (c *Client) CancelTask(ctx context.Context, taskID string) (*Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := map[string]string{"id": taskID}
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"1"`),
		Method:  MethodCancel,
		Params:  mustMarshal(params),
	}

	task, err := c.call(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// ── internal ──

func (c *Client) call(ctx context.Context, reqBody JSONRPCRequest) (*Task, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("a2a client: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("a2a client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		ts, sig := Sign(c.secret, time.Now())
		req.Header.Set(HeaderTimestamp, ts)
		req.Header.Set(HeaderSignature, sig)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a client: POST %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a client: HTTP %d from %s: %s", resp.StatusCode, c.url, strings.TrimSpace(string(body)))
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("a2a client: read response: %w", err)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respData, &rpcResp); err != nil {
		return nil, fmt.Errorf("a2a client: decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("a2a RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Result is the Task
	resultJSON, err := json.Marshal(rpcResp.Result)
	if err != nil {
		return nil, fmt.Errorf("a2a client: marshal result: %w", err)
	}

	var task Task
	if err := json.Unmarshal(resultJSON, &task); err != nil {
		return nil, fmt.Errorf("a2a client: decode task: %w", err)
	}

	return &task, nil
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}
