package natsbus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/security"
)

type Module struct {
	url                   string
	streamName            string
	user                  string
	password              string
	token                 string
	tlsCAFile             string
	tlsCertFile           string
	tlsKeyFile            string
	tlsServerName         string
	tlsInsecureSkipVerify bool

	mu     sync.RWMutex
	nc     *nats.Conn
	js     nats.JetStreamContext
	tracer trace.Tracer
}

func New(url, streamName, user, password, token, tlsCAFile, tlsCertFile, tlsKeyFile, tlsServerName string, tlsInsecureSkipVerify bool) *Module {
	return &Module{
		url: url, streamName: streamName,
		user: user, password: password, token: token,
		tlsCAFile: tlsCAFile, tlsCertFile: tlsCertFile, tlsKeyFile: tlsKeyFile, tlsServerName: tlsServerName, tlsInsecureSkipVerify: tlsInsecureSkipVerify,
		tracer: otel.Tracer("shiro.natsbus"),
	}
}

func (m *Module) Name() string { return "natsbus" }

func (m *Module) Start(ctx context.Context) error {
	tlsConfig, err := security.BuildTLSConfig(security.TLSOptions{
		CAFile: m.tlsCAFile, CertFile: m.tlsCertFile, KeyFile: m.tlsKeyFile, ServerName: m.tlsServerName, InsecureSkipVerify: m.tlsInsecureSkipVerify,
	})
	if err != nil {
		return err
	}

	opts := []nats.Option{
		nats.Name("shiro-distributed-system"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	if m.token != "" {
		opts = append(opts, nats.Token(m.token))
	} else if m.user != "" {
		opts = append(opts, nats.UserInfo(m.user, m.password))
	}
	if tlsConfig != nil {
		opts = append(opts, nats.Secure(tlsConfig))
	}

	nc, err := nats.Connect(
		m.url,
		opts...,
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

func (m *Module) Publish(ctx context.Context, subject string, data []byte, messageID string) (string, error) {
	ctx, span := m.tracer.Start(ctx, "nats.publish", trace.WithAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination", subject),
		attribute.String("messaging.message_id", messageID),
	))
	defer span.End()

	m.mu.RLock()
	js := m.js
	m.mu.RUnlock()
	if js == nil {
		return "", errors.New("jetstream not ready")
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	if messageID != "" {
		msg.Header.Set(nats.MsgIdHdr, messageID)
	}
	ack, err := js.PublishMsg(msg)
	if err != nil {
		return "", fmt.Errorf("publish %s: %w", subject, err)
	}
	span.SetAttributes(attribute.Int64("messaging.nats.sequence", int64(ack.Sequence)))
	return fmt.Sprintf("%s:%d", ack.Stream, ack.Sequence), nil
}

func (m *Module) Subscribe(ctx context.Context, subject string, handler func(modules.BusMessage)) (func() error, error) {
	m.mu.RLock()
	nc := m.nc
	m.mu.RUnlock()
	if nc == nil {
		return nil, errors.New("nats not connected")
	}

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		_, span := m.tracer.Start(context.Background(), "nats.consume", trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination", msg.Subject),
		))
		messageID := msg.Header.Get(nats.MsgIdHdr)
		if messageID == "" {
			if meta, err := msg.Metadata(); err == nil {
				messageID = fmt.Sprintf("%s:%d:%d", meta.Stream, meta.Sequence.Stream, meta.Sequence.Consumer)
			}
		}
		handler(modules.BusMessage{
			Subject:    msg.Subject,
			MessageID:  messageID,
			Data:       append([]byte(nil), msg.Data...),
			ReceivedAt: time.Now().UTC(),
		})
		span.End()
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
