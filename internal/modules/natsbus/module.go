package natsbus

import "context"

type Module struct {
	url string
}

func New(url string) *Module {
	return &Module{url: url}
}

func (m *Module) Name() string                    { return "natsbus" }
func (m *Module) Start(ctx context.Context) error { return nil }
func (m *Module) Ready(ctx context.Context) error { return nil }
func (m *Module) Stop(ctx context.Context) error  { return nil }
