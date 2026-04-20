package natsbus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type Module struct {
	url        string
	streamName string

	mu sync.RWMutex
	nc *nats.Conn
	js nats.JetStreamContext
}

func New(url, streamName string) *Module {
	return &Module{url: url, streamName: streamName}
}

func (m *Module) Name() string { return "natsbus" }

func (m *Module) Start(ctx context.Context) error {
	nc, err := nats.Connect(
		m.url,
		nats.Name("shiro-distributed-system"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return err
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return err
	}

	cfg := &nats.StreamConfig{
		Name:      m.streamName,
		Subjects:  []string{"events.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		Discard:   nats.DiscardOld,
		MaxAge:    7 * 24 * time.Hour,
	}

	if _, err := js.StreamInfo(m.streamName); err != nil {
		if _, addErr := js.AddStream(cfg); addErr != nil {
			nc.Close()
			return addErr
		}
	}

	m.mu.Lock()
	m.nc = nc
	m.js = js
	m.mu.Unlock()

	return nil
}

func (m *Module) Ready(ctx context.Context) error {
	m.mu.RLock()
	nc := m.nc
	m.mu.RUnlock()
	if nc == nil || !nc.IsConnected() {
		return errors.New("nats not connected")
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	nc := m.nc
	m.nc = nil
	m.js = nil
	m.mu.Unlock()

	if nc != nil {
		if err := nc.Drain(); err != nil {
			nc.Close()
			return err
		}
	}
	return nil
}

func (m *Module) Publish(ctx context.Context, subject string, data []byte) error {
	m.mu.RLock()
	js := m.js
	m.mu.RUnlock()
	if js == nil {
		return errors.New("jetstream not ready")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err := js.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

func (m *Module) Subscribe(ctx context.Context, subject string, handler func(modules.BusMessage)) (func() error, error) {
	m.mu.RLock()
	nc := m.nc
	m.mu.RUnlock()
	if nc == nil {
		return nil, errors.New("nats not connected")
	}

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		handler(modules.BusMessage{
			Subject:    msg.Subject,
			Data:       append([]byte(nil), msg.Data...),
			ReceivedAt: time.Now().UTC(),
		})
	})
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()

	return sub.Unsubscribe, nil
}
