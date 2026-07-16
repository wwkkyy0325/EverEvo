package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
)

// MsgHandler turns an incoming Feishu message into a reply. The Client wires it
// to the IM receive event and posts the returned text back to the same chat.
type MsgHandler func(ctx context.Context, chatID, text string) (string, error)

// Client is a Feishu long-connection bot: it dials out to Feishu over WebSocket,
// receives group-message events, runs the handler, and replies.
// Lifecycle mirrors internal/a2a/manager.go (done chan + Start/Stop).
type Client struct {
	cfg     Config
	handler MsgHandler
	lark    *lark.Client // for Im.Message.Create (reply)
	ws      *ws.Client

	startMu sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewClient builds a Feishu bot client. Call Start to connect.
func NewClient(cfg Config, handler MsgHandler) *Client {
	return &Client{
		cfg:     cfg,
		handler: handler,
		lark:    lark.NewClient(cfg.AppID, cfg.AppSecret),
		done:    make(chan struct{}),
	}
}

// Start connects to Feishu and begins dispatching message events. The WebSocket
// loop blocks, so it runs in a goroutine; Start returns once it is launched.
func (c *Client) Start(ctx context.Context) error {
	c.startMu.Lock()
	if c.started {
		c.startMu.Unlock()
		return fmt.Errorf("feishu: already started")
	}
	d := dispatcher.NewEventDispatcher(c.cfg.VerificationToken, "").
		OnP2MessageReceiveV1(c.onMessageReceive)
	c.ws = ws.NewClient(c.cfg.AppID, c.cfg.AppSecret, ws.WithEventHandler(d))

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.started = true
	c.startMu.Unlock()

	go func() {
		if err := c.ws.Start(runCtx); err != nil {
			log.Printf("[feishu] ws ended: %v", err)
		}
	}()
	log.Printf("[feishu] bot connecting (appID ...%s)", maskAppID(c.cfg.AppID))
	return nil
}

// Stop disconnects from Feishu. Safe to call multiple times.
func (c *Client) Stop() {
	c.startMu.Lock()
	if !c.started {
		c.startMu.Unlock()
		return
	}
	c.started = false
	wsCli := c.ws
	if c.cancel != nil {
		c.cancel()
	}
	c.startMu.Unlock()

	if wsCli != nil {
		wsCli.Close()
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	log.Println("[feishu] bot disconnected")
}

// Status reports the current connection state.
func (c *Client) Status() Status {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return Status{Running: c.started, AppID: maskAppID(c.cfg.AppID)}
}

// Done returns a channel closed when Stop completes (for lifecycle tests).
func (c *Client) Done() <-chan struct{} { return c.done }

// onMessageReceive is the Feishu event callback. It returns immediately so the
// SDK does not retry; the handler runs async (LLM calls can outlive the callback).
func (c *Client) onMessageReceive(_ context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}
	msg := event.Event.Message
	if msg.MessageType == nil || *msg.MessageType != "text" {
		return nil // v1: text only
	}
	chatID := ""
	if msg.ChatId != nil {
		chatID = *msg.ChatId
	}
	text := parseTextContent(msg.Content)
	if text == "" || chatID == "" {
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		reply, err := c.handler(ctx, chatID, text)
		if err != nil {
			reply = "处理失败: " + err.Error()
		}
		if reply == "" {
			return
		}
		if err := c.reply(ctx, chatID, reply); err != nil {
			log.Printf("[feishu] reply to %s: %v", chatID, err)
		}
	}()
	return nil
}

// reply posts a text message to the given chat.
func (c *Client) reply(ctx context.Context, chatID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	resp, err := c.lark.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu reply: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu reply: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// parseTextContent extracts the "text" field from a Feishu text-message content
// blob (arrives as a JSON string like {"text":"hello"}).
func parseTextContent(raw *string) string {
	if raw == nil || *raw == "" {
		return ""
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*raw), &body); err != nil {
		return ""
	}
	return body.Text
}

func maskAppID(appID string) string {
	if len(appID) <= 4 {
		return appID
	}
	return appID[len(appID)-4:]
}
