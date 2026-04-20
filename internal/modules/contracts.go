package modules

import (
	"context"
	"time"
)

type Module interface {
	Name() string
	Start(ctx context.Context) error
	Ready(ctx context.Context) error
	Stop(ctx context.Context) error
}

type BusMessage struct {
	Subject    string    `json:"subject"`
	Data       []byte    `json:"data"`
	ReceivedAt time.Time `json:"received_at"`
}

type EventRecord struct {
	ID         string    `json:"id"`
	Stream     string    `json:"stream"`
	Subject    string    `json:"subject"`
	Type       string    `json:"type"`
	Payload    string    `json:"payload"`
	OccurredAt time.Time `json:"occurred_at"`
}

type EventBus interface {
	Publish(ctx context.Context, subject string, data []byte) error
	Subscribe(ctx context.Context, subject string, handler func(BusMessage)) (func() error, error)
}

type Coordinator interface {
	IsLeader() bool
	LeaderID() string
}

type EventStore interface {
	AppendEvent(ctx context.Context, event EventRecord) (string, error)
	RecentEvents(ctx context.Context, stream string, limit int) ([]EventRecord, error)
}
