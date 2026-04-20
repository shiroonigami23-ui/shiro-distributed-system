package config

import (
	"os"
	"strings"
)

type Config struct {
	HTTPAddr              string
	NodeID                string
	NATSURL               string
	NATSStream            string
	NATSUser              string
	NATSPassword          string
	NATSToken             string
	EtcdEndpoints         []string
	EtcdUser              string
	EtcdPassword          string
	LeaderElectionKey     string
	CassandraHosts        []string
	CassandraKeyspace     string
	CassandraUser         string
	CassandraPassword     string
	TLSCAFile             string
	TLSCertFile           string
	TLSKeyFile            string
	TLSServerName         string
	TLSInsecureSkipVerify bool
	APIBearerToken        string
	APIPublishTokens      []string
	APIReadTokens         []string
	APIAdminTokens        []string
	DisableAPITokenAuth   bool
}

func FromEnv() Config {
	return Config{
		HTTPAddr:              envOr("HTTP_ADDR", ":8080"),
		NodeID:                envOr("NODE_ID", hostOr("node-1")),
		NATSURL:               envOr("NATS_URL", "nats://localhost:4222"),
		NATSStream:            envOr("NATS_STREAM", "SHIRO_EVENTS"),
		NATSUser:              envOr("NATS_USER", ""),
		NATSPassword:          envOr("NATS_PASSWORD", ""),
		NATSToken:             envOr("NATS_TOKEN", ""),
		EtcdEndpoints:         splitOr("ETCD_ENDPOINTS", "localhost:2379"),
		EtcdUser:              envOr("ETCD_USER", ""),
		EtcdPassword:          envOr("ETCD_PASSWORD", ""),
		LeaderElectionKey:     envOr("LEADER_ELECTION_KEY", "/shirods/controlplane/leader"),
		CassandraHosts:        splitOr("CASSANDRA_HOSTS", "localhost:9042"),
		CassandraKeyspace:     envOr("CASSANDRA_KEYSPACE", "shirods"),
		CassandraUser:         envOr("CASSANDRA_USER", ""),
		CassandraPassword:     envOr("CASSANDRA_PASSWORD", ""),
		TLSCAFile:             envOr("TLS_CA_FILE", ""),
		TLSCertFile:           envOr("TLS_CERT_FILE", ""),
		TLSKeyFile:            envOr("TLS_KEY_FILE", ""),
		TLSServerName:         envOr("TLS_SERVER_NAME", ""),
		TLSInsecureSkipVerify: boolOr("TLS_INSECURE_SKIP_VERIFY", false),
		APIBearerToken:        envOr("API_BEARER_TOKEN", ""),
		APIPublishTokens:      splitOr("API_ACL_PUBLISH_TOKENS", ""),
		APIReadTokens:         splitOr("API_ACL_READ_TOKENS", ""),
		APIAdminTokens:        splitOr("API_ACL_ADMIN_TOKENS", ""),
		DisableAPITokenAuth:   boolOr("DISABLE_API_TOKEN_AUTH", false),
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

func boolOr(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
