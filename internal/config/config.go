package config

import (
	"os"
	"strings"
)

type Config struct {
	HTTPAddr          string
	NodeID            string
	NATSURL           string
	NATSStream        string
	EtcdEndpoints     []string
	LeaderElectionKey string
	CassandraHosts    []string
	CassandraKeyspace string
}

func FromEnv() Config {
	return Config{
		HTTPAddr:          envOr("HTTP_ADDR", ":8080"),
		NodeID:            envOr("NODE_ID", hostOr("node-1")),
		NATSURL:           envOr("NATS_URL", "nats://localhost:4222"),
		NATSStream:        envOr("NATS_STREAM", "SHIRO_EVENTS"),
		EtcdEndpoints:     splitOr("ETCD_ENDPOINTS", "localhost:2379"),
		LeaderElectionKey: envOr("LEADER_ELECTION_KEY", "/shirods/controlplane/leader"),
		CassandraHosts:    splitOr("CASSANDRA_HOSTS", "localhost:9042"),
		CassandraKeyspace: envOr("CASSANDRA_KEYSPACE", "shirods"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitOr(key, fallback string) []string {
	v := envOr(key, fallback)
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hostOr(fallback string) string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fallback
}
