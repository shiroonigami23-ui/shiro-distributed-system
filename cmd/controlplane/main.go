package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/config"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/core"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules/cassstore"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules/etcdcoord"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules/natsbus"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.FromEnv()

	app := core.New(cfg,
		natsbus.New(cfg.NATSURL),
		etcdcoord.New(cfg.EtcdEndpoints),
		cassstore.New(cfg.CassandraHosts, cfg.CassandraKeyspace),
	)

	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
