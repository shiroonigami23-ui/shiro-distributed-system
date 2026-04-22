package cassstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
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

const allBucket = "all"

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
		`CREATE TABLE IF NOT EXISTS idempotency_by_created (
bucket text,
created_at timestamp,
stream text,
idempotency_key text,
event_id timeuuid,
PRIMARY KEY ((bucket), created_at, stream, idempotency_key)
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
		`CREATE TABLE IF NOT EXISTS outbox_retry_state (
event_id timeuuid PRIMARY KEY,
retry_count int,
next_attempt_at timestamp,
last_error text,
lease_owner text,
lease_until timestamp,
quarantined boolean,
quarantined_at timestamp,
first_failed_at timestamp
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
		`CREATE TABLE IF NOT EXISTS dead_letters_by_time (
bucket text,
quarantined_at timestamp,
event_id timeuuid,
stream text,
subject text,
event_type text,
payload text,
retry_count int,
last_error text,
PRIMARY KEY ((bucket), quarantined_at, event_id)
)`,
		`CREATE TABLE IF NOT EXISTS dead_letters_by_id (
event_id timeuuid PRIMARY KEY,
quarantined_at timestamp,
stream text,
subject text,
event_type text,
payload text,
retry_count int,
last_error text
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
		if err := session.Query(
			"INSERT INTO idempotency_by_created (bucket, created_at, stream, idempotency_key, event_id) VALUES (?, ?, ?, ?, ?)",
			allBucket, time.Now().UTC(), event.Stream, event.IdempotencyKey, newID,
		).WithContext(ctx).Exec(); err != nil {
			return modules.AppendResult{}, err
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
	batch.Query(
		"INSERT INTO outbox_retry_state (event_id, retry_count, next_attempt_at, last_error, lease_owner, lease_until, quarantined) VALUES (?, ?, ?, ?, ?, ?, ?)",
		newID, 0, event.OccurredAt, "", "", time.Unix(0, 0).UTC(), false,
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
	batch.Query(
		"DELETE FROM outbox_retry_state WHERE event_id = ?",
		id,
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
	if limit > 2000 {
		limit = 2000
	}
	fetchLimit := limit * 5
	if fetchLimit < 100 {
		fetchLimit = 100
	}

	iter := session.Query(
		"SELECT occurred_at, event_id, stream, subject, event_type, payload FROM outbox_by_state WHERE publish_state = ? LIMIT ?",
		"pending", fetchLimit,
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
		retryCount, nextAttemptAt, quarantined, leaseUntil, err := m.loadRetryState(ctx, session, id)
		if err != nil {
			continue
		}
		now := time.Now().UTC()
		if quarantined || nextAttemptAt.After(now) || leaseUntil.After(now) {
			continue
		}
		out = append(out, modules.EventRecord{
			ID:         id.String(),
			Stream:     stream,
			Subject:    subject,
			Type:       eventType,
			Payload:    payload,
			OccurredAt: occurredAt.UTC(),
			RetryCount: retryCount,
		})
		if len(out) >= limit {
			break
		}
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

func (m *Module) ClaimOutboxLease(ctx context.Context, eventID string, owner string, leaseDuration time.Duration) (bool, error) {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return false, errors.New("cassandra session not available")
	}
	id, err := gocql.ParseUUID(eventID)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseDuration)
	var currentLeaseUntil time.Time
	applied, err := session.Query(
		"UPDATE outbox_retry_state SET lease_owner = ?, lease_until = ? WHERE event_id = ? IF lease_until < ?",
		owner, leaseUntil, id, now,
	).WithContext(ctx).ScanCAS(&currentLeaseUntil)
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (m *Module) RecordOutboxFailure(
	ctx context.Context,
	eventID string,
	lastError string,
	baseBackoff time.Duration,
	maxBackoff time.Duration,
	jitterPercent int,
	quarantineAfter int,
) (bool, int, error) {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return false, 0, errors.New("cassandra session not available")
	}
	id, err := gocql.ParseUUID(eventID)
	if err != nil {
		return false, 0, err
	}
	var (
		retryCount  int
		firstFailed time.Time
	)
	err = session.Query(
		"SELECT retry_count, first_failed_at FROM outbox_retry_state WHERE event_id = ?",
		id,
	).WithContext(ctx).Scan(&retryCount, &firstFailed)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			retryCount = 0
		} else {
			return false, 0, err
		}
	}
	retryCount++
	wait := baseBackoff
	for i := 1; i < retryCount; i++ {
		wait *= 2
		if wait >= maxBackoff {
			wait = maxBackoff
			break
		}
	}
	if jitterPercent > 0 {
		jitterRange := int(wait.Milliseconds()) * jitterPercent / 100
		if jitterRange > 0 {
			delta := rand.Intn(jitterRange*2+1) - jitterRange
			wait += time.Duration(delta) * time.Millisecond
			if wait < 10*time.Millisecond {
				wait = 10 * time.Millisecond
			}
		}
	}
	now := time.Now().UTC()
	nextAttempt := now.Add(wait)
	quarantined := retryCount >= quarantineAfter
	if firstFailed.IsZero() {
		firstFailed = now
	}

	err = session.Query(
		"UPDATE outbox_retry_state SET retry_count = ?, next_attempt_at = ?, last_error = ?, lease_owner = ?, lease_until = ?, quarantined = ?, quarantined_at = ?, first_failed_at = ? WHERE event_id = ?",
		retryCount, nextAttempt, lastError, "", time.Unix(0, 0).UTC(), quarantined, now, firstFailed, id,
	).WithContext(ctx).Exec()
	if err != nil {
		return false, retryCount, err
	}
	if !quarantined {
		return false, retryCount, nil
	}

	var (
		stream    string
		subject   string
		eventType string
		payload   string
	)
	err = session.Query(
		"SELECT stream, subject, event_type, payload FROM outbox_events WHERE event_id = ?",
		id,
	).WithContext(ctx).Scan(&stream, &subject, &eventType, &payload)
	if err != nil {
		return true, retryCount, err
	}
	batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Query(
		"INSERT INTO dead_letters_by_time (bucket, quarantined_at, event_id, stream, subject, event_type, payload, retry_count, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		allBucket, now, id, stream, subject, eventType, payload, retryCount, lastError,
	)
	batch.Query(
		"INSERT INTO dead_letters_by_id (event_id, quarantined_at, stream, subject, event_type, payload, retry_count, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, now, stream, subject, eventType, payload, retryCount, lastError,
	)
	return true, retryCount, session.ExecuteBatch(batch)
}

func (m *Module) ListDeadLetters(ctx context.Context, limit int) ([]modules.EventRecord, error) {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return nil, errors.New("cassandra session not available")
	}
	if limit <= 0 {
		limit = 50
	}
	iter := session.Query(
		"SELECT quarantined_at, event_id, stream, subject, event_type, payload, retry_count, last_error FROM dead_letters_by_time WHERE bucket = ? LIMIT ?",
		allBucket, limit,
	).WithContext(ctx).Iter()
	out := make([]modules.EventRecord, 0, limit)
	var (
		quarantinedAt time.Time
		id            gocql.UUID
		stream        string
		subject       string
		eventType     string
		payload       string
		retryCount    int
		lastError     string
	)
	for iter.Scan(&quarantinedAt, &id, &stream, &subject, &eventType, &payload, &retryCount, &lastError) {
		out = append(out, modules.EventRecord{
			ID:            id.String(),
			Stream:        stream,
			Subject:       subject,
			Type:          eventType,
			Payload:       payload,
			RetryCount:    retryCount,
			LastError:     lastError,
			QuarantinedAt: quarantinedAt.UTC(),
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Module) ReplayDeadLetter(ctx context.Context, eventID string) error {
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
	var (
		quarantinedAt time.Time
		stream        string
		subject       string
		eventType     string
		payload       string
		retryCount    int
		lastError     string
	)
	err = session.Query(
		"SELECT quarantined_at, stream, subject, event_type, payload, retry_count, last_error FROM dead_letters_by_id WHERE event_id = ?",
		id,
	).WithContext(ctx).Scan(&quarantinedAt, &stream, &subject, &eventType, &payload, &retryCount, &lastError)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	var occurredAt time.Time
	err = session.Query(
		"SELECT occurred_at FROM outbox_events WHERE event_id = ?",
		id,
	).WithContext(ctx).Scan(&occurredAt)
	if err != nil {
		if !errors.Is(err, gocql.ErrNotFound) {
			return err
		}
		occurredAt = now
	}
	batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Query(
		"INSERT INTO outbox_events (event_id, stream, subject, event_type, payload, occurred_at, publish_state) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, stream, subject, eventType, payload, occurredAt, "pending",
	)
	batch.Query(
		"INSERT INTO outbox_by_state (publish_state, occurred_at, event_id, stream, subject, event_type, payload) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"pending", occurredAt, id, stream, subject, eventType, payload,
	)
	batch.Query(
		"UPDATE outbox_retry_state SET quarantined = ?, quarantined_at = ?, retry_count = ?, next_attempt_at = ?, last_error = ?, lease_owner = ?, lease_until = ? WHERE event_id = ?",
		false, time.Unix(0, 0).UTC(), retryCount, now, "", "", time.Unix(0, 0).UTC(), id,
	)
	batch.Query(
		"DELETE FROM dead_letters_by_time WHERE bucket = ? AND quarantined_at = ? AND event_id = ?",
		allBucket, quarantinedAt, id,
	)
	batch.Query(
		"DELETE FROM dead_letters_by_id WHERE event_id = ?",
		id,
	)
	return session.ExecuteBatch(batch)
}

func (m *Module) CleanupExpired(ctx context.Context, idempotencyBefore time.Time, deadLetterBefore time.Time, limit int) (int, int, error) {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return 0, 0, errors.New("cassandra session not available")
	}
	if limit <= 0 {
		limit = 200
	}
	idemDeleted := 0
	dlDeleted := 0

	type idemRow struct {
		createdAt      time.Time
		stream         string
		idempotencyKey string
	}
	idemRows := make([]idemRow, 0, limit)
	iter := session.Query(
		"SELECT created_at, stream, idempotency_key FROM idempotency_by_created WHERE bucket = ? LIMIT ?",
		allBucket, limit,
	).WithContext(ctx).Iter()
	var createdAt time.Time
	var stream, idKey string
	for iter.Scan(&createdAt, &stream, &idKey) {
		if createdAt.Before(idempotencyBefore) {
			idemRows = append(idemRows, idemRow{createdAt: createdAt, stream: stream, idempotencyKey: idKey})
		}
	}
	if err := iter.Close(); err != nil {
		return 0, 0, err
	}
	for _, row := range idemRows {
		batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Query("DELETE FROM idempotency_keys WHERE stream = ? AND idempotency_key = ?", row.stream, row.idempotencyKey)
		batch.Query("DELETE FROM idempotency_by_created WHERE bucket = ? AND created_at = ? AND stream = ? AND idempotency_key = ?", allBucket, row.createdAt, row.stream, row.idempotencyKey)
		if err := session.ExecuteBatch(batch); err == nil {
			idemDeleted++
		}
	}

	type dlRow struct {
		quarantinedAt time.Time
		eventID       gocql.UUID
	}
	dlRows := make([]dlRow, 0, limit)
	iter = session.Query(
		"SELECT quarantined_at, event_id FROM dead_letters_by_time WHERE bucket = ? LIMIT ?",
		allBucket, limit,
	).WithContext(ctx).Iter()
	var quarantinedAt time.Time
	var eventID gocql.UUID
	for iter.Scan(&quarantinedAt, &eventID) {
		if quarantinedAt.Before(deadLetterBefore) {
			dlRows = append(dlRows, dlRow{quarantinedAt: quarantinedAt, eventID: eventID})
		}
	}
	if err := iter.Close(); err != nil {
		return idemDeleted, 0, err
	}
	for _, row := range dlRows {
		batch := session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Query("DELETE FROM dead_letters_by_time WHERE bucket = ? AND quarantined_at = ? AND event_id = ?", allBucket, row.quarantinedAt, row.eventID)
		batch.Query("DELETE FROM dead_letters_by_id WHERE event_id = ?", row.eventID)
		if err := session.ExecuteBatch(batch); err == nil {
			dlDeleted++
		}
	}

	return idemDeleted, dlDeleted, nil
}

func (m *Module) loadRetryState(ctx context.Context, session *gocql.Session, eventID gocql.UUID) (int, time.Time, bool, time.Time, error) {
	var (
		retryCount  int
		nextAttempt time.Time
		quarantined bool
		leaseUntil  time.Time
	)
	err := session.Query(
		"SELECT retry_count, next_attempt_at, quarantined, lease_until FROM outbox_retry_state WHERE event_id = ?",
		eventID,
	).WithContext(ctx).Scan(&retryCount, &nextAttempt, &quarantined, &leaseUntil)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return 0, time.Unix(0, 0).UTC(), false, time.Unix(0, 0).UTC(), nil
		}
		return 0, time.Time{}, false, time.Time{}, err
	}
	return retryCount, nextAttempt, quarantined, leaseUntil, nil
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
