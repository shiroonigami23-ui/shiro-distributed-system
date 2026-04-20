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
	MessageID  string    `json:"message_id"`
	Data       []byte    `json:"data"`
	ReceivedAt time.Time `json:"received_at"`
}

type EventRecord struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Stream         string    `json:"stream"`
	Subject        string    `json:"subject"`
	Type           string    `json:"type"`
	Payload        string    `json:"payload"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type AppendResult struct {
	Event     EventRecord `json:"event"`
	Duplicate bool        `json:"duplicate"`
	Published bool        `json:"published"`
}

type EventBus interface {
	Publish(ctx context.Context, subject string, data []byte, messageID string) (string, error)
	Subscribe(ctx context.Context, subject string, handler func(BusMessage)) (func() error, error)
}

type Coordinator interface {
	IsLeader() bool
	LeaderID() string
}

type EventStore interface {
	AppendEventExactlyOnce(ctx context.Context, event EventRecord) (AppendResult, error)
	MarkOutboxPublished(ctx context.Context, eventID string, brokerMessageID string) error
	PendingOutboxEvents(ctx context.Context, limit int) ([]EventRecord, error)
	ClaimInboxMessage(ctx context.Context, consumer string, messageID string) (bool, error)
	RecentEvents(ctx context.Context, stream string, limit int) ([]EventRecord, error)
}
