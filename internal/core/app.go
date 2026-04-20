package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/config"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type App struct {
	cfg     config.Config
	modules []modules.Module

	bus   modules.EventBus
	coord modules.Coordinator
	store modules.EventStore

	reqs *prometheus.CounterVec
	lat  *prometheus.HistogramVec
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
	return a
}

func (a *App) Run(ctx context.Context) error {
	for _, m := range a.modules {
		if err := m.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", m.Name(), err)
		}
		log.Printf("module started: %s", m.Name())
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.Handle("/leaderz", a.secure("admin", http.HandlerFunc(a.handleLeader)))
	mux.Handle("/events", a.secure("rw", http.HandlerFunc(a.handleEvents)))
	mux.Handle("/stream", a.secure("read", http.HandlerFunc(a.handleStream)))

	srv := &http.Server{
		Addr:    a.cfg.HTTPAddr,
		Handler: a.instrument(mux),
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		path := r.URL.Path
		a.reqs.WithLabelValues(path, r.Method, strconv.Itoa(rec.status)).Inc()
		a.lat.WithLabelValues(path, r.Method).Observe(time.Since(start).Seconds())
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	tctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	for _, m := range a.modules {
		if err := m.Ready(tctx); err != nil {
			http.Error(w, m.Name()+": "+err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
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
	if a.bus == nil || a.store == nil {
		http.Error(w, "event bus/store not configured", http.StatusInternalServerError)
		return
	}

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
		brokerID, err := a.bus.Publish(r.Context(), result.Event.Subject, []byte(result.Event.Payload), result.Event.ID)
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
