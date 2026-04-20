package etcdcoord

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/security"
)

type Module struct {
	nodeID                string
	endpoints             []string
	electionKey           string
	user                  string
	password              string
	tlsCAFile             string
	tlsCertFile           string
	tlsKeyFile            string
	tlsServerName         string
	tlsInsecureSkipVerify bool

	mu       sync.RWMutex
	client   *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election
	cancel   context.CancelFunc

	isLeader atomic.Bool
	leaderID atomic.Value
}

func New(nodeID string, endpoints []string, electionKey, user, password, tlsCAFile, tlsCertFile, tlsKeyFile, tlsServerName string, tlsInsecureSkipVerify bool) *Module {
	m := &Module{
		nodeID:      nodeID,
		endpoints:   endpoints,
		electionKey: electionKey,
		user:        user, password: password,
		tlsCAFile: tlsCAFile, tlsCertFile: tlsCertFile, tlsKeyFile: tlsKeyFile, tlsServerName: tlsServerName, tlsInsecureSkipVerify: tlsInsecureSkipVerify,
	}
	m.leaderID.Store("")
	return m
}

func (m *Module) Name() string { return "etcdcoord" }

func (m *Module) Start(ctx context.Context) error {
	tlsConfig, err := security.BuildTLSConfig(security.TLSOptions{
		CAFile: m.tlsCAFile, CertFile: m.tlsCertFile, KeyFile: m.tlsKeyFile, ServerName: m.tlsServerName, InsecureSkipVerify: m.tlsInsecureSkipVerify,
	})
	if err != nil {
		return err
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   m.endpoints,
		DialTimeout: 5 * time.Second,
		Username:    m.user,
		Password:    m.password,
		TLS:         tlsConfig,
	})
	if err != nil {
		return err
	}

	session, err := concurrency.NewSession(client, concurrency.WithTTL(10))
	if err != nil {
		_ = client.Close()
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.client = client
	m.session = session
	m.election = concurrency.NewElection(session, m.electionKey)
	m.cancel = cancel
	m.mu.Unlock()

	go m.runElection(runCtx)
	go m.observeLeader(runCtx)

	return nil
}

func (m *Module) Ready(ctx context.Context) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return errors.New("etcd not connected")
	}
	statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := client.Status(statusCtx, m.endpoints[0])
	return err
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	election := m.election
	session := m.session
	client := m.client
	m.cancel = nil
	m.election = nil
	m.session = nil
	m.client = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if election != nil && m.isLeader.Load() {
		_ = election.Resign(ctx)
	}

	if session != nil {
		_ = session.Close()
	}
	if client != nil {
		return client.Close()
	}
	return nil
}

func (m *Module) IsLeader() bool {
	return m.isLeader.Load()
}

func (m *Module) LeaderID() string {
	v := m.leaderID.Load()
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (m *Module) runElection(ctx context.Context) {
	m.mu.RLock()
	election := m.election
	m.mu.RUnlock()
	if election == nil {
		return
	}

	if err := election.Campaign(ctx, m.nodeID); err != nil {
		return
	}
	m.isLeader.Store(true)
	m.leaderID.Store(m.nodeID)

	<-ctx.Done()
	m.isLeader.Store(false)
}

func (m *Module) observeLeader(ctx context.Context) {
	m.mu.RLock()
	election := m.election
	m.mu.RUnlock()
	if election == nil {
		return
	}

	ch := election.Observe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-ch:
			if !ok {
				return
			}
			if len(resp.Kvs) == 0 {
				continue
			}
			leader := string(resp.Kvs[0].Value)
			m.leaderID.Store(leader)
			m.isLeader.Store(leader == m.nodeID)
		}
	}
}
