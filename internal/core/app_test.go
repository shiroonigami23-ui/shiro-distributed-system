package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

type fakeModule struct {
	name     string
	readyErr error
}

func (f fakeModule) Name() string                    { return f.name }
func (f fakeModule) Start(ctx context.Context) error { return nil }
func (f fakeModule) Ready(ctx context.Context) error { return f.readyErr }
func (f fakeModule) Stop(ctx context.Context) error  { return nil }

func TestWithRequestID(t *testing.T) {
	a := &App{}
	h := a.withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := requestIDFromContext(r.Context()); got == "" {
			t.Fatalf("expected request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("expected X-Request-Id header")
	}
}

func TestWithConcurrencyLimitRejectsWhenFull(t *testing.T) {
	a := &App{guard: make(chan struct{}, 1)}
	a.guard <- struct{}{}
	defer func() { <-a.guard }()

	h := a.withConcurrencyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHandleLive(t *testing.T) {
	a := &App{cfg: config.Config{NodeID: "n1"}}
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	a.handleLive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"alive\"") {
		t.Fatalf("expected alive response, got %s", rec.Body.String())
	}
}

func TestHandleHealthDegraded(t *testing.T) {
	a := &App{
		cfg:     config.Config{NodeID: "n1"},
		modules: []modules.Module{fakeModule{name: "m1", readyErr: errors.New("down")}},
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	a.handleHealth(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"degraded\"") {
		t.Fatalf("expected degraded response, got %s", rec.Body.String())
	}
}

func TestHandleStartup(t *testing.T) {
	a := &App{cfg: config.Config{NodeID: "n1"}}
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	rec := httptest.NewRecorder()
	a.handleStartup(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before started, got %d", rec.Code)
	}
	a.started.Store(true)
	rec = httptest.NewRecorder()
	a.handleStartup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after started, got %d", rec.Code)
	}
}

func TestCircuitBreakerTransitions(t *testing.T) {
	b := newCircuitBreaker("test", 2, 50*time.Millisecond, 1)
	if !b.Allow() {
		t.Fatalf("expected closed breaker to allow")
	}
	b.Failure()
	if !b.Allow() {
		t.Fatalf("expected breaker to still allow after 1 failure")
	}
	b.Failure()
	if b.Allow() {
		t.Fatalf("expected breaker to open after threshold failures")
	}
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatalf("expected half-open breaker to allow probe request")
	}
	b.Success()
	if !b.Allow() {
		t.Fatalf("expected breaker to close after success in half-open")
	}
}
