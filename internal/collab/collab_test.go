package collab

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestEventBusPubSub verifies basic subscribe/publish delivery.
func TestEventBusPubSub(t *testing.T) {
	bus := NewEventBus(nil)
	sub := bus.Subscribe("agent.ag1.done")
	bus.Publish("agent.ag1.done", Event{Source: "ag1", Type: "done", Payload: "ok"})
	select {
	case ev := <-sub.Recv():
		if ev.Source != "ag1" {
			t.Fatalf("expected source ag1, got %s", ev.Source)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// TestEventBusPrefixMatch verifies "agent.*" prefix subscriptions.
func TestEventBusPrefixMatch(t *testing.T) {
	bus := NewEventBus(nil)
	sub := bus.Subscribe("agent.*")
	bus.Publish("agent.xyz.done", Event{Source: "xyz"})
	select {
	case <-sub.Recv():
		// ok
	case <-time.After(time.Second):
		t.Fatal("prefix subscription did not receive event")
	}
}

// TestEventBusUnsubscribe verifies no delivery after unsubscribe.
func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus(nil)
	sub := bus.Subscribe("topic")
	bus.Unsubscribe("topic", sub)
	bus.Publish("topic", Event{})
	// After unsubscribe the channel is closed: receive should yield the
	// zero value immediately with ok==false.
	select {
	case _, ok := <-sub.Recv():
		if ok {
			t.Fatal("received event after unsubscribe")
		}
		// ok == false: channel closed, as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("unsubscribe did not close the channel")
	}
}

// TestBlackboardConcurrent verifies 100 goroutines can Set/Get without races.
func TestBlackboardConcurrent(t *testing.T) {
	bus := NewEventBus(nil)
	bb := NewBlackboard("bb_test", bus)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bb.Set("key", "v", "author", KindText)
			bb.Get("key")
		}(i)
	}
	wg.Wait()
	if bb.Size() != 1 {
		t.Fatalf("expected 1 entry, got %d", bb.Size())
	}
}

// TestBlackboardSetPublish verifies writes emit a bus event.
func TestBlackboardSetPublish(t *testing.T) {
	bus := NewEventBus(nil)
	bb := NewBlackboard("bb_pub", bus)
	sub := bus.Subscribe("blackboard.bb_pub.changed")
	bb.Set("k", "v", "ag1", KindText)
	select {
	case ev := <-sub.Recv():
		if ev.Type != "set" {
			t.Fatalf("expected type set, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("blackboard write did not publish event")
	}
}

// TestKernelAgentRun verifies the run lifecycle + Done channel.
func TestKernelAgentRun(t *testing.T) {
	k := NewKernel(nil)
	r := &AgentRun{
		ID: "run1", AgentID: "ag1", Status: StatusRunning,
		StartedAt: time.Now(), Done: make(chan struct{}),
	}
	if !k.RegisterRun(r) {
		t.Fatal("RegisterRun returned false")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		k.CompleteRun("run1", "result", nil)
	}()
	select {
	case <-r.Done:
	case <-time.After(time.Second):
		t.Fatal("Done channel never closed")
	}
	if r.Status != StatusDone || r.Result != "result" {
		t.Fatalf("unexpected run state: %+v", r)
	}
}

// TestDispatcherLocal verifies in-process routing (no HTTP).
func TestDispatcherLocal(t *testing.T) {
	k := NewKernel(nil)
	runner := stubRunner{}
	d := NewDispatcher(k, runner, nil)
	d.RegisterLocal("ag-local")
	out, err := d.Send(nil, "ag-local", "hello", "")
	if err != nil {
		t.Fatalf("local send failed: %v", err)
	}
	if out != "stub:hello" {
		t.Fatalf("unexpected output: %s", out)
	}
}

// stubRunner is a test AgentRunner.
type stubRunner struct{}

func (stubRunner) RunAgent(_ context.Context, _, task, _ string) (string, error) {
	return "stub:" + task, nil
}
