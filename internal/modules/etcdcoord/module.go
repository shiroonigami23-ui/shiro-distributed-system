package etcdcoord

import "context"

type Module struct {
	endpoints []string
}

func New(endpoints []string) *Module {
	return &Module{endpoints: endpoints}
}

func (m *Module) Name() string                    { return "etcdcoord" }
func (m *Module) Start(ctx context.Context) error { return nil }
func (m *Module) Ready(ctx context.Context) error { return nil }
func (m *Module) Stop(ctx context.Context) error  { return nil }
