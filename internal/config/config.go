package config

import "os"

type Config struct {
	HTTPAddr          string
	NATSURL           string
	EtcdEndpoints     []string
	CassandraHosts    []string
	CassandraKeyspace string
}

func FromEnv() Config {
	return Config{
		HTTPAddr:          envOr("HTTP_ADDR", ":8080"),
		NATSURL:           envOr("NATS_URL", "nats://localhost:4222"),
		EtcdEndpoints:     splitOr("ETCD_ENDPOINTS", "localhost:2379"),
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
	var out []string
	start := 0
	for i := 0; i <= len(v); i++ {
		if i == len(v) || v[i] == ',' {
			if i > start {
				out = append(out, v[start:i])
			}
			start = i + 1
		}
	}
	return out
}
