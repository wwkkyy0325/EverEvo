package a2a

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// e2e_test.go exercises the full HTTP path: a real Server behind httptest, and a
// real Client talking to it — covering signed task round-trip, signature
// rejection, and backward-compat when no secret is configured.

func testCard() AgentCard {
	return AgentCard{
		Name:               "Test Agent",
		Capabilities:       AgentCapabilities{StateTransitionHistory: true},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
}

func TestE2ESignedTaskRoundTrip(t *testing.T) {
	executor := func(ctx context.Context, msgs []Message) (string, error) { return "pong", nil }
	srv := NewServer(testCard(), executor, "topsecret")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "topsecret")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	task, err := client.SendTask(context.Background(), TextMessage("user", "ping"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatalf("expected artifact, got %+v", task)
	}
	if got := task.Artifacts[0].Parts[0].Text; got != "pong" {
		t.Fatalf("expected pong, got %q", got)
	}
}

func TestE2ERejectsUnsigned(t *testing.T) {
	executor := func(ctx context.Context, msgs []Message) (string, error) { return "x", nil }
	srv := NewServer(testCard(), executor, "topsecret")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// client without a secret does not sign; discovery (agent-card) is public...
	client := NewClient(ts.URL, "")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect (card is public): %v", err)
	}
	// ...but the task endpoint must reject it.
	_, err := client.SendTask(context.Background(), TextMessage("user", "ping"))
	if err == nil {
		t.Fatal("expected send to be rejected (unsigned), got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized") && !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestE2ERejectsWrongSecret(t *testing.T) {
	executor := func(ctx context.Context, msgs []Message) (string, error) { return "x", nil }
	srv := NewServer(testCard(), executor, "secret-a")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "secret-b") // mismatched secret → signature mismatch
	if _, err := client.SendTask(context.Background(), TextMessage("user", "ping")); err == nil {
		t.Fatal("expected signature-mismatch rejection, got nil")
	}
}

func TestE2ENoSecretBackwardCompat(t *testing.T) {
	executor := func(ctx context.Context, msgs []Message) (string, error) { return "ok", nil }
	srv := NewServer(testCard(), executor, "") // no secret → verification disabled
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	task, err := client.SendTask(context.Background(), TextMessage("user", "ping"))
	if err != nil {
		t.Fatalf("send (backward compat): %v", err)
	}
	if len(task.Artifacts) == 0 || task.Artifacts[0].Parts[0].Text != "ok" {
		t.Fatalf("unexpected task: %+v", task)
	}
}
