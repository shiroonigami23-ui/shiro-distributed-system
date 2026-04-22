package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr                  string
	HTTPReadTimeoutMs         int
	HTTPReadHeaderTimeoutMs   int
	HTTPWriteTimeoutMs        int
	HTTPIdleTimeoutMs         int
	HTTPShutdownTimeoutMs     int
	HTTPRequestTimeoutMs      int
	MaxConcurrentRequests     int
	MaxConcurrentPublishes    int
	MaxConcurrentQueries      int
	CircuitFailureThreshold   int
	CircuitOpenMs             int
	CircuitHalfOpenSuccesses  int
	PublishTimeoutMs          int
	OutboxLeaseMs             int
	OutboxBackoffBaseMs       int
	OutboxBackoffMaxMs        int
	OutboxBackoffJitterPct    int
	OutboxQuarantineAfter     int
	CleanupIntervalMs         int
	CleanupBatchSize          int
	IdempotencyTTLHours       int
	DeadLetterTTLHours        int
	SubjectRateLimitRPS       int
	SubjectRateLimitBurst     int
	SubjectRateLimitOverrides map[string]int
	SchemaVersionRules        map[string]VersionRange
	SchemaRequiredFields      map[string][]string
	ModuleStartRetryMax       int
	ModuleStartRetryBackoffMs int
	NodeID                    string
	NATSURL                   string
	NATSStream                string
	NATSUser                  string
	NATSPassword              string
	NATSToken                 string
	EtcdEndpoints             []string
	EtcdUser                  string
	EtcdPassword              string
	LeaderElectionKey         string
	CassandraHosts            []string
	CassandraKeyspace         string
	CassandraUser             string
	CassandraPassword         string
	TLSCAFile                 string
	TLSCertFile               string
	TLSKeyFile                string
	TLSServerName             string
	TLSInsecureSkipVerify     bool
	APIBearerToken            string
	APIPublishTokens          []string
	APIReadTokens             []string
	APIAdminTokens            []string
	DisableAPITokenAuth       bool
	PublishRetryMax           int
	PublishRetryBackoffMs     int
	PublishRetryMaxBackoffMs  int
	OutboxRelayIntervalMs     int
	OutboxRelayBatchSize      int
	RateLimitRPS              int
	RateLimitBurst            int
	RateLimitMaxIPs           int
	RateLimitIPTTLSeconds     int
	MaxRequestBodyBytes       int64
	SecurityHeadersEnabled    bool
	AuditLogEnabled           bool
	RequireLeaderForWrites    bool
	OTELServiceName           string
	OTELExporterOTLPEndpoint  string
}

func FromEnv() Config {
	return Config{
		HTTPAddr:                  envOr("HTTP_ADDR", ":8080"),
		HTTPReadTimeoutMs:         intOr("HTTP_READ_TIMEOUT_MS", 10000),
		HTTPReadHeaderTimeoutMs:   intOr("HTTP_READ_HEADER_TIMEOUT_MS", 3000),
		HTTPWriteTimeoutMs:        intOr("HTTP_WRITE_TIMEOUT_MS", 15000),
		HTTPIdleTimeoutMs:         intOr("HTTP_IDLE_TIMEOUT_MS", 60000),
		HTTPShutdownTimeoutMs:     intOr("HTTP_SHUTDOWN_TIMEOUT_MS", 10000),
		HTTPRequestTimeoutMs:      intOr("HTTP_REQUEST_TIMEOUT_MS", 8000),
		MaxConcurrentRequests:     intOr("MAX_CONCURRENT_REQUESTS", 500),
		MaxConcurrentPublishes:    intOr("MAX_CONCURRENT_PUBLISHES", 200),
		MaxConcurrentQueries:      intOr("MAX_CONCURRENT_QUERIES", 300),
		CircuitFailureThreshold:   intOr("CIRCUIT_FAILURE_THRESHOLD", 5),
		CircuitOpenMs:             intOr("CIRCUIT_OPEN_MS", 15000),
		CircuitHalfOpenSuccesses:  intOr("CIRCUIT_HALF_OPEN_SUCCESSES", 2),
		PublishTimeoutMs:          intOr("PUBLISH_TIMEOUT_MS", 5000),
		OutboxLeaseMs:             intOr("OUTBOX_LEASE_MS", 15000),
		OutboxBackoffBaseMs:       intOr("OUTBOX_BACKOFF_BASE_MS", 500),
		OutboxBackoffMaxMs:        intOr("OUTBOX_BACKOFF_MAX_MS", 30000),
		OutboxBackoffJitterPct:    intOr("OUTBOX_BACKOFF_JITTER_PCT", 25),
		OutboxQuarantineAfter:     intOr("OUTBOX_QUARANTINE_AFTER", 12),
		CleanupIntervalMs:         intOr("CLEANUP_INTERVAL_MS", 30000),
		CleanupBatchSize:          intOr("CLEANUP_BATCH_SIZE", 200),
		IdempotencyTTLHours:       intOr("IDEMPOTENCY_TTL_HOURS", 24*14),
		DeadLetterTTLHours:        intOr("DEAD_LETTER_TTL_HOURS", 24*30),
		SubjectRateLimitRPS:       intOr("SUBJECT_RATE_LIMIT_RPS", 100),
		SubjectRateLimitBurst:     intOr("SUBJECT_RATE_LIMIT_BURST", 200),
		SubjectRateLimitOverrides: parseIntMap("SUBJECT_RATE_LIMIT_RPS_OVERRIDES"),
		SchemaVersionRules:        parseSchemaVersionRules("EVENT_SCHEMA_VERSION_RULES"),
		SchemaRequiredFields:      parseRequiredFieldRules("EVENT_SCHEMA_REQUIRED_FIELDS"),
		ModuleStartRetryMax:       intOr("MODULE_START_RETRY_MAX", 4),
		ModuleStartRetryBackoffMs: intOr("MODULE_START_RETRY_BACKOFF_MS", 1000),
		NodeID:                    envOr("NODE_ID", hostOr("node-1")),
		NATSURL:                   envOr("NATS_URL", "nats://localhost:4222"),
		NATSStream:                envOr("NATS_STREAM", "SHIRO_EVENTS"),
		NATSUser:                  envOr("NATS_USER", ""),
		NATSPassword:              envOr("NATS_PASSWORD", ""),
		NATSToken:                 envOr("NATS_TOKEN", ""),
		EtcdEndpoints:             splitOr("ETCD_ENDPOINTS", "localhost:2379"),
		EtcdUser:                  envOr("ETCD_USER", ""),
		EtcdPassword:              envOr("ETCD_PASSWORD", ""),
		LeaderElectionKey:         envOr("LEADER_ELECTION_KEY", "/shirods/controlplane/leader"),
		CassandraHosts:            splitOr("CASSANDRA_HOSTS", "localhost:9042"),
		CassandraKeyspace:         envOr("CASSANDRA_KEYSPACE", "shirods"),
		CassandraUser:             envOr("CASSANDRA_USER", ""),
		CassandraPassword:         envOr("CASSANDRA_PASSWORD", ""),
		TLSCAFile:                 envOr("TLS_CA_FILE", ""),
		TLSCertFile:               envOr("TLS_CERT_FILE", ""),
		TLSKeyFile:                envOr("TLS_KEY_FILE", ""),
		TLSServerName:             envOr("TLS_SERVER_NAME", ""),
		TLSInsecureSkipVerify:     boolOr("TLS_INSECURE_SKIP_VERIFY", false),
		APIBearerToken:            envOr("API_BEARER_TOKEN", ""),
		APIPublishTokens:          splitOr("API_ACL_PUBLISH_TOKENS", ""),
		APIReadTokens:             splitOr("API_ACL_READ_TOKENS", ""),
		APIAdminTokens:            splitOr("API_ACL_ADMIN_TOKENS", ""),
		DisableAPITokenAuth:       boolOr("DISABLE_API_TOKEN_AUTH", false),
		PublishRetryMax:           intOr("PUBLISH_RETRY_MAX", 4),
		PublishRetryBackoffMs:     intOr("PUBLISH_RETRY_BACKOFF_MS", 150),
		PublishRetryMaxBackoffMs:  intOr("PUBLISH_RETRY_MAX_BACKOFF_MS", 3000),
		OutboxRelayIntervalMs:     intOr("OUTBOX_RELAY_INTERVAL_MS", 1500),
		OutboxRelayBatchSize:      intOr("OUTBOX_RELAY_BATCH_SIZE", 100),
		RateLimitRPS:              intOr("RATE_LIMIT_RPS", 60),
		RateLimitBurst:            intOr("RATE_LIMIT_BURST", 120),
		RateLimitMaxIPs:           intOr("RATE_LIMIT_MAX_IPS", 10000),
		RateLimitIPTTLSeconds:     intOr("RATE_LIMIT_IP_TTL_SECONDS", 900),
		MaxRequestBodyBytes:       int64Or("MAX_REQUEST_BODY_BYTES", 1<<20),
		SecurityHeadersEnabled:    boolOr("SECURITY_HEADERS_ENABLED", true),
		AuditLogEnabled:           boolOr("AUDIT_LOG_ENABLED", true),
		RequireLeaderForWrites:    boolOr("REQUIRE_LEADER_FOR_WRITES", false),
		OTELServiceName:           envOr("OTEL_SERVICE_NAME", "shiro-distributed-system"),
		OTELExporterOTLPEndpoint:  envOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
}

type VersionRange struct {
	Min int
	Max int
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

func intOr(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func int64Or(key string, fallback int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func parseIntMap(key string) map[string]int {
	raw := strings.TrimSpace(os.Getenv(key))
	out := map[string]int{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		out[k] = n
	}
	return out
}

func parseSchemaVersionRules(key string) map[string]VersionRange {
	raw := strings.TrimSpace(os.Getenv(key))
	out := map[string]VersionRange{}
	if raw == "" {
		return out
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		evt := strings.TrimSpace(parts[0])
		rng := strings.TrimSpace(parts[1])
		bounds := strings.SplitN(rng, "-", 2)
		if len(bounds) != 2 {
			continue
		}
		minV, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
		maxV, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
		if err1 != nil || err2 != nil || minV <= 0 || maxV < minV {
			continue
		}
		out[evt] = VersionRange{Min: minV, Max: maxV}
	}
	return out
}

func parseRequiredFieldRules(key string) map[string][]string {
	raw := strings.TrimSpace(os.Getenv(key))
	out := map[string][]string{}
	if raw == "" {
		return out
	}
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		evt := strings.TrimSpace(parts[0])
		if evt == "" {
			continue
		}
		fields := []string{}
		for _, f := range strings.Split(parts[1], "|") {
			f = strings.TrimSpace(f)
			if f != "" {
				fields = append(fields, f)
			}
		}
		if len(fields) > 0 {
			out[evt] = fields
		}
	}
	return out
}
