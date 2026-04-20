package modules

import "context"

type Module interface {
	Name() string
	Start(ctx context.Context) error
	Ready(ctx context.Context) error
	Stop(ctx context.Context) error
}
