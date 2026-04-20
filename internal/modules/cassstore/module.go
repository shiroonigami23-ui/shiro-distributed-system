package cassstore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type Module struct {
	hosts    []string
	keyspace string

	mu      sync.RWMutex
	session *gocql.Session
}

func New(hosts []string, keyspace string) *Module {
	return &Module{hosts: hosts, keyspace: keyspace}
}

func (m *Module) Name() string { return "cassstore" }

func (m *Module) Start(ctx context.Context) error {
	hosts, port := normalizeHosts(m.hosts)
	cluster := gocql.NewCluster(hosts...)
	cluster.Port = port
	cluster.Timeout = 8 * time.Second
	cluster.ConnectTimeout = 8 * time.Second
	cluster.Consistency = gocql.Quorum

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

	tableCQL := `CREATE TABLE IF NOT EXISTS events_by_stream (
stream text,
occurred_at timestamp,
event_id timeuuid,
subject text,
event_type text,
payload text,
PRIMARY KEY ((stream), occurred_at, event_id)
) WITH CLUSTERING ORDER BY (occurred_at DESC, event_id DESC)`
	if err := session.Query(tableCQL).WithContext(ctx).Exec(); err != nil {
		session.Close()
		return err
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

func (m *Module) AppendEvent(ctx context.Context, event modules.EventRecord) (string, error) {
	m.mu.RLock()
	session := m.session
	m.mu.RUnlock()
	if session == nil {
		return "", errors.New("cassandra session not available")
	}

	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	id := gocql.TimeUUID()
	err := session.Query(
		"INSERT INTO events_by_stream (stream, occurred_at, event_id, subject, event_type, payload) VALUES (?, ?, ?, ?, ?, ?)",
		event.Stream, event.OccurredAt, id, event.Subject, event.Type, event.Payload,
	).WithContext(ctx).Exec()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (m *Module) RecentEvents(ctx context.Context, stream string, limit int) ([]modules.EventRecord, error) {
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
	defer iter.Close()

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
	return events, nil
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
