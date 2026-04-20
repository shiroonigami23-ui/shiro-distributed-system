package cassstore

import "context"

type Module struct {
	hosts    []string
	keyspace string
}

func New(hosts []string, keyspace string) *Module {
	return &Module{hosts: hosts, keyspace: keyspace}
}

func (m *Module) Name() string                    { return "cassstore" }
func (m *Module) Start(ctx context.Context) error { return nil }
func (m *Module) Ready(ctx context.Context) error { return nil }
func (m *Module) Stop(ctx context.Context) error  { return nil }
