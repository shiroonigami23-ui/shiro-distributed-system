package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/config"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type App struct {
	cfg     config.Config
	modules []modules.Module

	bus   modules.EventBus
	coord modules.Coordinator
	store modules.EventStore

	reqs        *prometheus.CounterVec
	lat         *prometheus.HistogramVec
	pubRetries  prometheus.Counter
	pubFailures prometheus.Counter
	tracer      trace.Tracer
	limiter     *ipRateLimiter
	guard       chan struct{}
	pubGuard    chan struct{}
	queryGuard  chan struct{}
	started     atomic.Bool
}

type publishRequest struct {
	Stream         string          `json:"stream"`
	Subject        string          `json:"subject"`
	Type           string          `json:"type"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

func New(cfg config.Config, ms ...modules.Module) *App {
	a := &App{cfg: cfg, modules: ms}
	for _, m := range ms {
		if bus, ok := m.(modules.EventBus); ok {
			a.bus = bus
		}
		if coord, ok := m.(modules.Coordinator); ok {
			a.coord = coord
		}
		if store, ok := m.(modules.EventStore); ok {
			a.store = store
		}
	}
	a.reqs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "shiro_http_requests_total",
		Help: "Total HTTP requests grouped by path and method",
	}, []string{"path", "method", "status"})
	a.lat = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "shiro_http_request_duration_seconds",
		Help:    "HTTP request duration seconds grouped by path and method",
		Buckets: prometheus.DefBuckets,
	}, []string{"path", "method"})
	a.pubRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "shiro_publish_retries_total",
		Help: "Total publish retry attempts",
	})
	a.pubFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "shiro_publish_failures_total",
		Help: "Total publish failures after retries",
	})
	a.tracer = otel.Tracer("shiro.core")
	a.limiter = newIPRateLimiter(
		rate.Limit(max(1, cfg.RateLimitRPS)),
		max(1, cfg.RateLimitBurst),
		max(100, cfg.RateLimitMaxIPs),
		time.Duration(max(60, cfg.RateLimitIPTTLSeconds))*time.Second,
	)
	a.guard = make(chan struct{}, max(1, cfg.MaxConcurrentRequests))
	a.pubGuard = make(chan struct{}, max(1, cfg.MaxConcurrentPublishes))
	a.queryGuard = make(chan struct{}, max(1, cfg.MaxConcurrentQueries))
	return a
}

func (a *App) Run(ctx context.Context) error {
	for _, m := range a.modules {
		if err := a.startModuleWithRetry(ctx, m); err != nil {
			return fmt.Errorf("start %s: %w", m.Name(), err)
		}
		log.Printf("module started: %s", m.Name())
	}
	a.started.Store(true)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/readyz", a.handleHealth)
	mux.HandleFunc("/livez", a.handleLive)
	mux.HandleFunc("/startupz", a.handleStartup)
	mux.Handle("/leaderz", a.secure("admin", http.HandlerFunc(a.handleLeader)))
	mux.Handle("/events", a.secure("rw", http.HandlerFunc(a.handleEvents)))
	mux.Handle("/stream", a.secure("read", http.HandlerFunc(a.handleStream)))

	if a.store != nil && a.bus != nil {
		go a.runOutboxRelay(ctx)
	}

	srv := &http.Server{
		Addr:              a.cfg.HTTPAddr,
		Handler:           a.withRecovery(a.withSecurityHeaders(a.withRequestID(a.withRequestTimeout(a.withConcurrencyLimit(a.withRateLimit(a.instrument(mux))))))),
		ReadTimeout:       time.Duration(max(1, a.cfg.HTTPReadTimeoutMs)) * time.Millisecond,
		ReadHeaderTimeout: time.Duration(max(1, a.cfg.HTTPReadHeaderTimeoutMs)) * time.Millisecond,
		WriteTimeout:      time.Duration(max(1, a.cfg.HTTPWriteTimeoutMs)) * time.Millisecond,
		IdleTimeout:       time.Duration(max(1, a.cfg.HTTPIdleTimeoutMs)) * time.Millisecond,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("control plane listening on %s", a.cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(max(1, a.cfg.HTTPShutdownTimeoutMs))*time.Millisecond)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	for i := len(a.modules) - 1; i >= 0; i-- {
		m := a.modules[i]
		if err := m.Stop(shutdownCtx); err != nil {
			log.Printf("module stop warning [%s]: %v", m.Name(), err)
		}
	}
	return nil
}

func (a *App) secure(scope string, next http.Handler) http.Handler {
	if a.cfg.DisableAPITokenAuth {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" && a.cfg.APIBearerToken == "" && len(a.cfg.APIAdminTokens) == 0 && len(a.cfg.APIPublishTokens) == 0 && len(a.cfg.APIReadTokens) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		if token == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		if a.cfg.APIBearerToken != "" && token == a.cfg.APIBearerToken {
			next.ServeHTTP(w, r)
			return
		}

		allowed := false
		switch scope {
		case "admin":
			allowed = contains(a.cfg.APIAdminTokens, token)
		case "read":
			allowed = contains(a.cfg.APIReadTokens, token) || contains(a.cfg.APIAdminTokens, token)
		case "rw":
			allowed = contains(a.cfg.APIPublishTokens, token) || contains(a.cfg.APIReadTokens, token) || contains(a.cfg.APIAdminTokens, token)
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := appTracer(a).Start(r.Context(), "http "+r.Method+" "+r.URL.Path, trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		))
		defer span.End()

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))
		path := r.URL.Path
		a.reqs.WithLabelValues(path, r.Method, strconv.Itoa(rec.status)).Inc()
		a.lat.WithLabelValues(path, r.Method).Observe(time.Since(start).Seconds())
		span.SetAttributes(attribute.Int("http.status_code", rec.status))
		if rec.status >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
		if a.cfg.AuditLogEnabled {
			log.Printf("audit request_id=%s method=%s path=%s status=%d duration_ms=%d remote=%s", requestIDFromContext(r.Context()), r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds(), r.RemoteAddr)
		}
	})
}

func (a *App) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r.RemoteAddr)
		if !a.limiter.Allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) withRequestTimeout(next http.Handler) http.Handler {
	timeout := time.Duration(max(1, a.cfg.HTTPRequestTimeoutMs)) * time.Millisecond
	timeoutHandler := http.TimeoutHandler(next, timeout, "request timeout")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SSE stream endpoints are long-lived and should not be bounded by request timeout.
		if r.URL.Path == "/stream" {
			next.ServeHTTP(w, r)
			return
		}
		timeoutHandler.ServeHTTP(w, r)
	})
}

func (a *App) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.SecurityHeadersEnabled {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) withConcurrencyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case a.guard <- struct{}{}:
			defer func() { <-a.guard }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "server busy", http.StatusServiceUnavailable)
		}
	})
}

func (a *App) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered path=%s err=%v stack=%s", r.URL.Path, rec, string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *App) startModuleWithRetry(ctx context.Context, m modules.Module) error {
	maxAttempts := max(1, a.cfg.ModuleStartRetryMax)
	wait := time.Duration(max(100, a.cfg.ModuleStartRetryBackoffMs)) * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := m.Start(ctx); err == nil {
			return nil
		} else {
			lastErr = err
			log.Printf("module start attempt failed module=%s attempt=%d/%d err=%v", m.Name(), attempt, maxAttempts, err)
		}
		if attempt == maxAttempts {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if wait < 10*time.Second {
			wait *= 2
			if wait > 10*time.Second {
				wait = 10 * time.Second
			}
		}
	}
	return lastErr
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	tctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	type moduleStatus struct {
		Name      string `json:"name"`
		Ready     bool   `json:"ready"`
		Error     string `json:"error,omitempty"`
		LatencyMs int64  `json:"latency_ms"`
	}
	statuses := make([]moduleStatus, 0, len(a.modules))
	overallReady := true
	for _, m := range a.modules {
		start := time.Now()
		if err := m.Ready(tctx); err != nil {
			overallReady = false
			statuses = append(statuses, moduleStatus{
				Name:      m.Name(),
				Ready:     false,
				Error:     err.Error(),
				LatencyMs: time.Since(start).Milliseconds(),
			})
			continue
		}
		statuses = append(statuses, moduleStatus{
			Name:      m.Name(),
			Ready:     true,
			LatencyMs: time.Since(start).Milliseconds(),
		})
	}
	code := http.StatusOK
	if !overallReady {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":  map[bool]string{true: "ok", false: "degraded"}[overallReady],
		"node_id": a.cfg.NodeID,
		"time":    time.Now().UTC(),
		"modules": statuses,
	})
}

func (a *App) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "alive",
		"node_id": a.cfg.NodeID,
		"time":    time.Now().UTC(),
	})
}

func (a *App) handleStartup(w http.ResponseWriter, r *http.Request) {
	if !a.started.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  "starting",
			"node_id": a.cfg.NodeID,
			"time":    time.Now().UTC(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"node_id": a.cfg.NodeID,
		"time":    time.Now().UTC(),
	})
}

func (a *App) handleLeader(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"node_id":   a.cfg.NodeID,
		"is_leader": false,
		"leader_id": "",
	}
	if a.coord != nil {
		resp["is_leader"] = a.coord.IsLeader()
		resp["leader_id"] = a.coord.LeaderID()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.handlePublishEvent(w, r)
	case http.MethodGet:
		a.handleListEvents(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handlePublishEvent(w http.ResponseWriter, r *http.Request) {
	if !acquireGuard(a.pubGuard) {
		http.Error(w, "publish lane busy", http.StatusServiceUnavailable)
		return
	}
	defer releaseGuard(a.pubGuard)

	if a.bus == nil || a.store == nil {
		http.Error(w, "event bus/store not configured", http.StatusInternalServerError)
		return
	}
	if a.cfg.RequireLeaderForWrites && a.coord != nil && !a.coord.IsLeader() {
		http.Error(w, "write rejected on follower; leader="+a.coord.LeaderID(), http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxRequestBodyBytes)
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.Stream == "" {
		req.Stream = "default"
	}
	if req.Subject == "" {
		req.Subject = "events." + req.Stream
	}
	if req.Type == "" {
		req.Type = "event"
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}

	event := modules.EventRecord{
		IdempotencyKey: req.IdempotencyKey,
		Stream:         req.Stream,
		Subject:        req.Subject,
		Type:           req.Type,
		Payload:        string(req.Payload),
		OccurredAt:     time.Now().UTC(),
	}

	result, err := a.store.AppendEventExactlyOnce(r.Context(), event)
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !result.Published {
		brokerID, err := a.publishWithRetry(r.Context(), result.Event.Subject, []byte(result.Event.Payload), result.Event.ID)
		if err != nil {
			http.Error(w, "publish error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := a.store.MarkOutboxPublished(r.Context(), result.Event.ID, brokerID); err != nil {
			http.Error(w, "outbox update error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"id":             result.Event.ID,
		"stream":         result.Event.Stream,
		"subject":        result.Event.Subject,
		"type":           result.Event.Type,
		"occurredAt":     result.Event.OccurredAt,
		"idempotencyKey": result.Event.IdempotencyKey,
		"duplicate":      result.Duplicate,
	})
}

func (a *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	if !acquireGuard(a.queryGuard) {
		http.Error(w, "query lane busy", http.StatusServiceUnavailable)
		return
	}
	defer releaseGuard(a.queryGuard)

	if a.store == nil {
		http.Error(w, "event store not configured", http.StatusInternalServerError)
		return
	}
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = "default"
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	events, err := a.store.RecentEvents(r.Context(), stream, limit)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stream": stream,
		"count":  len(events),
		"events": events,
	})
}

func (a *App) handleStream(w http.ResponseWriter, r *http.Request) {
	if a.bus == nil {
		http.Error(w, "event bus not configured", http.StatusInternalServerError)
		return
	}

	subject := r.URL.Query().Get("subject")
	if subject == "" {
		subject = "events.>"
	}
	consumer := r.URL.Query().Get("consumer")
	if consumer == "" {
		consumer = "sse:" + a.cfg.NodeID
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan modules.BusMessage, 128)
	unsub, err := a.bus.Subscribe(r.Context(), subject, func(msg modules.BusMessage) {
		select {
		case ch <- msg:
		default:
		}
	})
	if err != nil {
		http.Error(w, "subscribe error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = unsub() }()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if a.store != nil && msg.MessageID != "" {
				ok, err := a.store.ClaimInboxMessage(r.Context(), consumer, msg.MessageID)
				if err != nil || !ok {
					continue
				}
			}
			body := map[string]any{
				"subject":   msg.Subject,
				"messageId": msg.MessageID,
				"data":      string(msg.Data),
				"ts":        msg.ReceivedAt,
			}
			raw, _ := json.Marshal(body)
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
			flusher.Flush()
		}
	}
}

func (a *App) publishWithRetry(ctx context.Context, subject string, payload []byte, messageID string) (string, error) {
	ctx, span := appTracer(a).Start(ctx, "publishWithRetry", trace.WithAttributes(
		attribute.String("messaging.destination", subject),
		attribute.String("messaging.message_id", messageID),
	))
	defer span.End()

	attempts := a.cfg.PublishRetryMax
	if attempts <= 0 {
		attempts = 1
	}
	backoff := time.Duration(a.cfg.PublishRetryBackoffMs) * time.Millisecond
	if backoff <= 0 {
		backoff = 150 * time.Millisecond
	}
	maxBackoff := time.Duration(a.cfg.PublishRetryMaxBackoffMs) * time.Millisecond
	if maxBackoff <= 0 {
		maxBackoff = 3 * time.Second
	}

	var lastErr error
	wait := backoff
	for attempt := 1; attempt <= attempts; attempt++ {
		id, err := a.bus.Publish(ctx, subject, payload, messageID)
		if err == nil {
			span.SetAttributes(attribute.Int("publish.attempt", attempt))
			return id, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}
		if a.pubRetries != nil {
			a.pubRetries.Inc()
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			span.RecordError(ctx.Err())
			return "", ctx.Err()
		case <-timer.C:
		}
		wait *= 2
		if wait > maxBackoff {
			wait = maxBackoff
		}
	}

	if a.pubFailures != nil {
		a.pubFailures.Inc()
	}
	// Best-effort dead-letter publish for offline analysis.
	dlq := "events.dlq"
	if subject != "" {
		dlq = subject + ".dlq"
	}
	dlqMsg := map[string]any{
		"subject":   subject,
		"messageId": messageID,
		"error":     lastErr.Error(),
		"payload":   string(payload),
		"failedAt":  time.Now().UTC(),
	}
	if raw, err := json.Marshal(dlqMsg); err == nil {
		_, _ = a.bus.Publish(context.Background(), dlq, raw, messageID+"-dlq")
	}
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "publish failed")
	return "", lastErr
}

func (a *App) runOutboxRelay(ctx context.Context) {
	interval := time.Duration(max(250, a.cfg.OutboxRelayIntervalMs)) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.coord != nil && !a.coord.IsLeader() {
				continue
			}
			rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			_ = a.relayOutboxBatch(rctx)
			cancel()
		}
	}
}

func (a *App) relayOutboxBatch(ctx context.Context) error {
	if a.store == nil || a.bus == nil {
		return nil
	}
	ctx, span := appTracer(a).Start(ctx, "relayOutboxBatch")
	defer span.End()

	batchSize := max(1, a.cfg.OutboxRelayBatchSize)
	pending, err := a.store.PendingOutboxEvents(ctx, batchSize)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Int("outbox.pending", len(pending)))
	for _, evt := range pending {
		brokerID, err := a.publishWithRetry(ctx, evt.Subject, []byte(evt.Payload), evt.ID)
		if err != nil {
			continue
		}
		if err := a.store.MarkOutboxPublished(ctx, evt.ID, brokerID); err != nil {
			span.RecordError(err)
		}
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func bearerToken(h string) string {
	if h == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if strings.TrimSpace(v) == value {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type ipRateLimiter struct {
	r             rate.Limit
	b             int
	maxIPs        int
	ttl           time.Duration
	nextSweep     time.Time
	mu            sync.Mutex
	data          map[string]*ipLimiterEntry
	lastEvictedIP string
}

type ipLimiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(r rate.Limit, b int, maxIPs int, ttl time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		r:         r,
		b:         b,
		maxIPs:    max(100, maxIPs),
		ttl:       ttl,
		nextSweep: time.Now().Add(time.Minute),
		data:      make(map[string]*ipLimiterEntry),
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	if now.After(l.nextSweep) {
		l.sweepLocked(now)
	}
	entry, ok := l.data[ip]
	if !ok {
		if len(l.data) >= l.maxIPs {
			l.evictOldestLocked()
		}
		entry = &ipLimiterEntry{
			lim:      rate.NewLimiter(l.r, l.b),
			lastSeen: now,
		}
		l.data[ip] = entry
	}
	entry.lastSeen = now
	l.mu.Unlock()
	return entry.lim.Allow()
}

func (l *ipRateLimiter) sweepLocked(now time.Time) {
	if l.ttl <= 0 {
		l.nextSweep = now.Add(time.Minute)
		return
	}
	cutoff := now.Add(-l.ttl)
	for ip, entry := range l.data {
		if entry.lastSeen.Before(cutoff) {
			delete(l.data, ip)
		}
	}
	l.nextSweep = now.Add(time.Minute)
}

func (l *ipRateLimiter) evictOldestLocked() {
	var (
		oldestIP   string
		oldestSeen time.Time
		first      = true
	)
	for ip, entry := range l.data {
		if first || entry.lastSeen.Before(oldestSeen) {
			first = false
			oldestIP = ip
			oldestSeen = entry.lastSeen
		}
	}
	if oldestIP != "" {
		delete(l.data, oldestIP)
		l.lastEvictedIP = oldestIP
	}
}

func clientIP(remoteAddr string) string {
	addrPort, err := netip.ParseAddrPort(remoteAddr)
	if err == nil {
		return addrPort.Addr().String()
	}
	parts := strings.Split(remoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return remoteAddr
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func appTracer(a *App) trace.Tracer {
	if a != nil && a.tracer != nil {
		return a.tracer
	}
	return otel.Tracer("shiro.core")
}

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b)
}

func acquireGuard(ch chan struct{}) bool {
	select {
	case ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseGuard(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
}
