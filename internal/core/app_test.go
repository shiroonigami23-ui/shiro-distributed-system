package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/config"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type fakeBus struct {
	failuresBeforeSuccess int
	publishCalls          int
	lastSubject           string
}

func (f *fakeBus) Publish(ctx context.Context, subject string, data []byte, messageID string) (string, error) {
	f.publishCalls++
	f.lastSubject = subject
	if f.failuresBeforeSuccess > 0 {
		f.failuresBeforeSuccess--
		return "", errors.New("transient publish error")
	}
	return "stream:1", nil
}

func (f *fakeBus) Subscribe(ctx context.Context, subject string, handler func(modules.BusMessage)) (func() error, error) {
	return func() error { return nil }, nil
}

func TestBearerToken(t *testing.T) {
	if got := bearerToken("Bearer abc123"); got != "abc123" {
		t.Fatalf("expected token abc123, got %q", got)
	}
	if got := bearerToken("Basic abc123"); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", " b "}, "b") {
		t.Fatalf("expected token list to contain b")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Fatalf("expected token list not to contain c")
	}
}

func TestPublishWithRetryEventuallySucceeds(t *testing.T) {
	b := &fakeBus{failuresBeforeSuccess: 2}
	a := &App{
		cfg: config.Config{
			PublishRetryMax:          4,
			PublishRetryBackoffMs:    1,
			PublishRetryMaxBackoffMs: 2,
		},
		bus: b,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	id, err := a.publishWithRetry(ctx, "events.orders", []byte(`{"k":"v"}`), "m1")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty broker id")
	}
	if b.publishCalls != 3 {
		t.Fatalf("expected 3 publish attempts, got %d", b.publishCalls)
	}
}

func TestPublishWithRetryFails(t *testing.T) {
	b := &fakeBus{failuresBeforeSuccess: 99}
	a := &App{
		cfg: config.Config{
			PublishRetryMax:          2,
			PublishRetryBackoffMs:    1,
			PublishRetryMaxBackoffMs: 2,
		},
		bus: b,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := a.publishWithRetry(ctx, "events.orders", []byte(`{"k":"v"}`), "m2")
	if err == nil {
		t.Fatalf("expected publish failure")
	}
	// 2 main attempts + 1 best-effort DLQ attempt
	if b.publishCalls != 3 {
		t.Fatalf("expected 3 publish calls including DLQ, got %d", b.publishCalls)
	}
	if b.lastSubject != "events.orders.dlq" {
		t.Fatalf("expected last subject to be DLQ, got %q", b.lastSubject)
	}
}
