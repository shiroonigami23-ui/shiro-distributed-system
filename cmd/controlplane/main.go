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
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	shutdownOtel, err := telemetry.Setup(ctx, cfg.OTELServiceName, cfg.OTELExporterOTLPEndpoint)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = shutdownOtel(context.Background())
	}()

	app := core.New(cfg,
		natsbus.New(cfg.NATSURL, cfg.NATSStream, cfg.NATSUser, cfg.NATSPassword, cfg.NATSToken, cfg.TLSCAFile, cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSServerName, cfg.TLSInsecureSkipVerify),
		etcdcoord.New(cfg.NodeID, cfg.EtcdEndpoints, cfg.LeaderElectionKey, cfg.EtcdUser, cfg.EtcdPassword, cfg.TLSCAFile, cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSServerName, cfg.TLSInsecureSkipVerify),
		cassstore.New(cfg.CassandraHosts, cfg.CassandraKeyspace, cfg.CassandraUser, cfg.CassandraPassword, cfg.TLSCAFile, cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSServerName, cfg.TLSInsecureSkipVerify),
	)

	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
