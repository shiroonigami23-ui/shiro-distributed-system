package cassstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/security"
)

type Module struct {
	hosts                 []string
	keyspace              string
	user                  string
	password              string
	tlsCAFile             string
	tlsCertFile           string
	tlsKeyFile            string
	tlsServerName         string
	tlsInsecureSkipVerify bool

	mu      sync.RWMutex
	session *gocql.Session
	tracer  trace.Tracer
}

func New(hosts []string, keyspace, user, password, tlsCAFile, tlsCertFile, tlsKeyFile, tlsServerName string, tlsInsecureSkipVerify bool) *Module {
	return &Module{
		hosts: hosts, keyspace: keyspace,
		user: user, password: password,
		tlsCAFile: tlsCAFile, tlsCertFile: tlsCertFile, tlsKeyFile: tlsKeyFile, tlsServerName: tlsServerName, tlsInsecureSkipVerify: tlsInsecureSkipVerify,
		tracer: otel.Tracer("shiro.cassstore"),
	}
}

func (m *Module) Name() string { return "cassstore" }

func (m *Module) Start(ctx context.Context) error {
	hosts, port := normalizeHosts(m.hosts)
	cluster := gocql.NewCluster(hosts...)
	cluster.Port = port
	cluster.Timeout = 8 * time.Second
	cluster.ConnectTimeout = 8 * time.Second
	cluster.Consistency = gocql.Quorum

	if m.user != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: m.user, Password: m.password}
	}
	tlsConfig, err := security.BuildTLSConfig(security.TLSOptions{
		CAFile: m.tlsCAFile, CertFile: m.tlsCertFile, KeyFile: m.tlsKeyFile, ServerName: m.tlsServerName, InsecureSkipVerify: m.tlsInsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	if tlsConfig != nil {
		cluster.SslOpts = &gocql.SslOptions{
			Config:                 tlsConfig,
			EnableHostVerification: !m.tlsInsecureSkipVerify,
		}
	}

	bootstrap, err := cluster.CreateSession()
	if err != nil {
		return err
	}
	defer bootstrap.Close()

	keyspaceCQL := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class':'SimpleStrategy','replication_factor':1}",
		m.keyspace,
	)
	if err := bootstrap.Query(keyspaceCQL).WithContext(ctx).Exec(); err != nil {
		return err
	}

	cluster.Keyspace = m.keyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return err
	}

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS events_by_stream (
stream text,
occurred_at timestamp,
event_id timeuuid,
subject text,
event_type text,
payload text,
PRIMARY KEY ((stream), occurred_at, event_id)
) WITH CLUSTERING ORDER BY (occurred_at DESC, event_id DESC)`,
		`CREATE TABLE IF NOT EXISTS events_by_id (
event_id timeuuid PRIMARY KEY,
stream text,
occurred_at timestamp,
subject text,
event_type text,
payload text
)`,
		`CREATE TABLE IF NOT EXISTS idempotency_keys (
stream text,
idempotency_key text,
event_id timeuuid,
payload_hash text,
created_at timestamp,
PRIMARY KEY ((stream), idempotency_key)
)`,
		`CREATE TABLE IF NOT EXISTS outbox_events (
event_id timeuuid PRIMARY KEY,
stream text,
subject text,
event_type text,
payload text,
occurred_at timestamp,
publish_state text,
published_at timestamp,
broker_message_id text
)`,
		`CREATE TABLE IF NOT EXISTS outbox_by_state (
publish_state text,
occurred_at timestamp,
event_id timeuuid,
stream text,
subject text,
event_type text,
payload text,
PRIMARY KEY ((publish_state), occurred_at, event_id)
) WITH CLUSTERING ORDER BY (occurred_at ASC, event_id ASC)`,
		`CREATE TABLE IF NOT EXISTS inbox_consumed (
consumer text,
message_id text,
consumed_at timestamp,
PRIMARY KEY ((consumer), message_id)
)`,
	}
	for _, cql := range ddl {
		if err := session.Query(cql).WithContext(ctx).Exec(); err != nil {
			session.Close()
			return err
		}
	}

	m.mu.Lock()
	m.session = session
	m.mu.Unlock()
	return nil
}

func (m *Module) Ready(ctx context.Context) error {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return errors.New("cassandra session not available")
	}
	return session.Query("SELECT key FROM system.local LIMIT 1").WithContext(ctx).Exec()
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	session := m.session
	m.session = nil
	m.mu.Unlock()
	if session != nil {
		session.Close()
	}
	return nil
}

func (m *Module) AppendEventExactlyOnce(ctx context.Context, event modules.EventRecord) (modules.AppendResult, error) {
	ctx, span := m.tracer.Start(ctx, "cass.appendEvent", trace.WithAttributes(
		attribute.String("db.system", "cassandra"),
		attribute.String("event.stream", event.Stream),
	))
	defer span.End()

	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return modules.AppendResult{}, errors.New("cassandra session not available")
	}
	if event.Stream == "" {
		event.Stream = "default"
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}

	payloadHash := sha256Hex(event.Payload)
	newID := gocql.TimeUUID()

	if event.IdempotencyKey != "" {
		var existingID gocql.UUID
		applied, err := session.Query(
			"INSERT INTO idempotency_keys (stream, idempotency_key, event_id, payload_hash, created_at) VALUES (?, ?, ?, ?, ?) IF NOT EXISTS",
			event.Stream, event.IdempotencyKey, newID, payloadHash, time.Now().UTC(),
		).WithContext(ctx).ScanCAS(&existingID)
		if err != nil {
			return modules.AppendResult{}, err
		}
		if !applied {
			var existing modules.EventRecord
			var rowID gocql.UUID
			err := session.Query(
				"SELECT event_id, stream, occurred_at, subject, event_type, payload FROM events_by_id WHERE event_id = ?",
				existingID,
			).WithContext(ctx).Scan(&rowID, &existing.Stream, &existing.OccurredAt, &existing.Subject, &existing.Type, &existing.Payload)
			if err != nil {
				return modules.AppendResult{}, err
			}
			existing.ID = rowID.String()
			existing.IdempotencyKey = event.IdempotencyKey
			published, err := m.isOutboxPublished(ctx, session, existingID)
			if err != nil {
				return modules.AppendResult{}, err
			}
			return modules.AppendResult{Event: existing, Duplicate: true, Published: published}, nil
		}
	}

	batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Query(
		"INSERT INTO events_by_stream (stream, occurred_at, event_id, subject, event_type, payload) VALUES (?, ?, ?, ?, ?, ?)",
		event.Stream, event.OccurredAt, newID, event.Subject, event.Type, event.Payload,
	)
	batch.Query(
		"INSERT INTO events_by_id (event_id, stream, occurred_at, subject, event_type, payload) VALUES (?, ?, ?, ?, ?, ?)",
		newID, event.Stream, event.OccurredAt, event.Subject, event.Type, event.Payload,
	)
	batch.Query(
		"INSERT INTO outbox_events (event_id, stream, subject, event_type, payload, occurred_at, publish_state) VALUES (?, ?, ?, ?, ?, ?, ?)",
		newID, event.Stream, event.Subject, event.Type, event.Payload, event.OccurredAt, "pending",
	)
	batch.Query(
		"INSERT INTO outbox_by_state (publish_state, occurred_at, event_id, stream, subject, event_type, payload) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"pending", event.OccurredAt, newID, event.Stream, event.Subject, event.Type, event.Payload,
	)
	if err := session.ExecuteBatch(batch); err != nil {
		return modules.AppendResult{}, err
	}

	event.ID = newID.String()
	span.SetAttributes(attribute.String("event.id", event.ID))
	return modules.AppendResult{Event: event, Duplicate: false, Published: false}, nil
}

func (m *Module) MarkOutboxPublished(ctx context.Context, eventID string, brokerMessageID string) error {
	ctx, span := m.tracer.Start(ctx, "cass.markOutboxPublished", trace.WithAttributes(
		attribute.String("db.system", "cassandra"),
		attribute.String("event.id", eventID),
	))
	defer span.End()

	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return errors.New("cassandra session not available")
	}
	id, err := gocql.ParseUUID(eventID)
	if err != nil {
		return err
	}
	var occurredAt time.Time
	var stream, subject, eventType, payload string
	err = session.Query(
		"SELECT occurred_at, stream, subject, event_type, payload FROM outbox_events WHERE event_id = ?",
		id,
	).WithContext(ctx).Scan(&occurredAt, &stream, &subject, &eventType, &payload)
	if err != nil {
		return err
	}

	batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Query(
		"UPDATE outbox_events SET publish_state = ?, published_at = ?, broker_message_id = ? WHERE event_id = ?",
		"published", time.Now().UTC(), brokerMessageID, id,
	)
	batch.Query(
		"DELETE FROM outbox_by_state WHERE publish_state = ? AND occurred_at = ? AND event_id = ?",
		"pending", occurredAt, id,
	)
	return session.ExecuteBatch(batch)
}

func (m *Module) PendingOutboxEvents(ctx context.Context, limit int) ([]modules.EventRecord, error) {
	ctx, span := m.tracer.Start(ctx, "cass.pendingOutbox", trace.WithAttributes(
		attribute.String("db.system", "cassandra"),
		attribute.Int("outbox.limit", limit),
	))
	defer span.End()

	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return nil, errors.New("cassandra session not available")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	iter := session.Query(
		"SELECT occurred_at, event_id, stream, subject, event_type, payload FROM outbox_by_state WHERE publish_state = ? LIMIT ?",
		"pending", limit,
	).WithContext(ctx).Iter()

	out := make([]modules.EventRecord, 0, limit)
	var (
		occurredAt time.Time
		id         gocql.UUID
		stream     string
		subject    string
		eventType  string
		payload    string
	)
	for iter.Scan(&occurredAt, &id, &stream, &subject, &eventType, &payload) {
		out = append(out, modules.EventRecord{
			ID:         id.String(),
			Stream:     stream,
			Subject:    subject,
			Type:       eventType,
			Payload:    payload,
			OccurredAt: occurredAt.UTC(),
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Int("outbox.pending_count", len(out)))
	return out, nil
}

func (m *Module) ClaimInboxMessage(ctx context.Context, consumer string, messageID string) (bool, error) {
	ctx, span := m.tracer.Start(ctx, "cass.claimInboxMessage", trace.WithAttributes(
		attribute.String("db.system", "cassandra"),
		attribute.String("consumer", consumer),
		attribute.String("message.id", messageID),
	))
	defer span.End()

	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return false, errors.New("cassandra session not available")
	}
	var consumedAt time.Time
	applied, err := session.Query(
		"INSERT INTO inbox_consumed (consumer, message_id, consumed_at) VALUES (?, ?, ?) IF NOT EXISTS",
		consumer, messageID, time.Now().UTC(),
	).WithContext(ctx).ScanCAS(&consumedAt)
	span.SetAttributes(attribute.Bool("inbox.applied", applied))
	return applied, err
}

func (m *Module) RecentEvents(ctx context.Context, stream string, limit int) ([]modules.EventRecord, error) {
	ctx, span := m.tracer.Start(ctx, "cass.recentEvents", trace.WithAttributes(
		attribute.String("db.system", "cassandra"),
		attribute.String("event.stream", stream),
		attribute.Int("query.limit", limit),
	))
	defer span.End()

	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return nil, errors.New("cassandra session not available")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	iter := session.Query(
		"SELECT event_id, occurred_at, subject, event_type, payload FROM events_by_stream WHERE stream = ? LIMIT ?",
		stream, limit,
	).WithContext(ctx).Iter()
	events := make([]modules.EventRecord, 0, limit)
	var (
		id         gocql.UUID
		occurredAt time.Time
		subject    string
		eventType  string
		payload    string
	)
	for iter.Scan(&id, &occurredAt, &subject, &eventType, &payload) {
		events = append(events, modules.EventRecord{
			ID:         id.String(),
			Stream:     stream,
			Subject:    subject,
			Type:       eventType,
			Payload:    payload,
			OccurredAt: occurredAt.UTC(),
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Int("query.count", len(events)))
	return events, nil
}

func (m *Module) isOutboxPublished(ctx context.Context, session *gocql.Session, eventID gocql.UUID) (bool, error) {
	var state string
	err := session.Query("SELECT publish_state FROM outbox_events WHERE event_id = ?", eventID).WithContext(ctx).Scan(&state)
	if err != nil {
		return false, err
	}
	return state == "published", nil
}

func sha256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func normalizeHosts(in []string) ([]string, int) {
	hosts := make([]string, 0, len(in))
	port := 9042
	for _, h := range in {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		host, p, err := net.SplitHostPort(h)
		if err == nil {
			hosts = append(hosts, host)
			if parsed, parseErr := strconv.Atoi(p); parseErr == nil {
				port = parsed
			}
			continue
		}
		hosts = append(hosts, h)
	}
	if len(hosts) == 0 {
		hosts = append(hosts, "localhost")
	}
	return hosts, port
}
