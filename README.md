# Shiro Distributed System

<p align="center">
  <img src="assets/images/cloud-computing-layers.png" alt="Cloud Layers" width="720" />
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white"></a>
  <img alt="License" src="https://img.shields.io/badge/License-Apache--2.0-111111?style=for-the-badge">
  <img alt="Architecture" src="https://img.shields.io/badge/Architecture-Modular-0A66C2?style=for-the-badge&logo=dependabot&logoColor=white">
  <img alt="Messaging" src="https://img.shields.io/badge/NATS-JetStream-27AE60?style=for-the-badge">
  <img alt="Coordination" src="https://img.shields.io/badge/etcd-Leader%20Election-34495E?style=for-the-badge">
  <img alt="Storage" src="https://img.shields.io/badge/Cassandra-Outbox%2FInbox-E67E22?style=for-the-badge">
  <img alt="API" src="https://img.shields.io/badge/API-REST%20%2B%20SSE-8E44AD?style=for-the-badge">
</p>

<p align="center">
  <a href="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/hardening-ci.yml"><img alt="Hardening CI" src="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/hardening-ci.yml/badge.svg"></a>
  <a href="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/checklist-gate.yml"><img alt="Checklist Gate" src="https://github.com/shiroonigami23-ui/shiro-distributed-system/actions/workflows/checklist-gate.yml/badge.svg"></a>
  <img alt="Last Commit" src="https://img.shields.io/github/last-commit/shiroonigami23-ui/shiro-distributed-system">
  <img alt="Repo Size" src="https://img.shields.io/github/repo-size/shiroonigami23-ui/shiro-distributed-system">
</p>

A production-grade distributed-system control plane focused on secure coordination, reliable eventing, and durable state.

## Core Features
- Real NATS + JetStream event transport
- Real etcd leader election
- Real Cassandra durable event store
- Exactly-once/idempotent publish flow (idempotency key + outbox state)
- Background outbox relay worker for eventual publish after restart/failure
- Inbox de-dup for consumers
- API auth + ACL enforcement
- Shared mTLS support across module connections
- Configurable publish retry/backoff with dead-letter fallback
- OpenTelemetry tracing (HTTP + NATS + Cassandra spans)
- Rate limiting, request size limits, and structured audit logs
- Panic recovery middleware with stack logging
- Configurable HTTP read/write/idle/request/shutdown timeouts
- Optional leader-only writes (`REQUIRE_LEADER_FOR_WRITES`) to avoid split-brain API writes
- Bounded per-IP limiter state (TTL + max tracked IPs) to resist memory growth attacks
- Request ID propagation (`X-Request-Id`) and audit correlation
- Global request concurrency guard
- Dedicated publish/query bulkhead lanes
- Security headers middleware
- Module startup retry with exponential backoff
- Dependency circuit breakers (NATS, Cassandra, module readiness paths)
- Outbox leasing to prevent duplicate relay processing in multi-node deployments
- Persistent retry counters + backoff/jitter state in Cassandra
- Poison-event quarantine and dead-letter replay endpoint
- Automated cleanup for expired idempotency keys and aged dead letters
- Type/schema version checks and required-field validation for payloads
- Per-subject write rate limits
- Prometheus metrics endpoint `/metrics`
- Kubernetes deployment + HPA + ServiceMonitor manifests
- CI workflow for formatting/build/test gates

## API Endpoints
- `GET /healthz` readiness report (JSON per-module status)
- `GET /readyz` readiness alias
- `GET /livez` liveness status
- `GET /startupz` startup status
- `GET /metrics` Prometheus metrics
- `GET /leaderz` current leader and node leadership (admin scope)
- `POST /events` persist + publish event with idempotency (rw scope)
- `GET /events?stream=<name>&limit=<n>` list recent events (rw scope)
- `GET /deadletters?limit=<n>` list quarantined dead-letter events (admin scope)
- `POST /deadletters/replay?id=<event_id>` replay quarantined event back to outbox (admin scope)
- `GET /stream?subject=events.>&consumer=<name>` live event stream with inbox dedup (read scope)

## Auth + ACL
Use one global bearer token or per-scope ACL tokens.

Headers:
- `Authorization: Bearer <token>`
- optional for writes: `Idempotency-Key: <client-generated-key>`

Environment examples:
- `API_BEARER_TOKEN`
- `API_ACL_ADMIN_TOKENS`
- `API_ACL_PUBLISH_TOKENS`
- `API_ACL_READ_TOKENS`
- `DISABLE_API_TOKEN_AUTH=false`
- `REQUIRE_LEADER_FOR_WRITES=false`

## mTLS + Credentials
Supported module credentials:
- `NATS_USER`, `NATS_PASSWORD`, `NATS_TOKEN`
- `ETCD_USER`, `ETCD_PASSWORD`
- `CASSANDRA_USER`, `CASSANDRA_PASSWORD`

Shared TLS options:
- `TLS_CA_FILE`
- `TLS_CERT_FILE`
- `TLS_KEY_FILE`
- `TLS_SERVER_NAME`
- `TLS_INSECURE_SKIP_VERIFY`

HTTP safety knobs:
- `HTTP_READ_TIMEOUT_MS`
- `HTTP_READ_HEADER_TIMEOUT_MS`
- `HTTP_WRITE_TIMEOUT_MS`
- `HTTP_IDLE_TIMEOUT_MS`
- `HTTP_REQUEST_TIMEOUT_MS` (skips `/stream` SSE)
- `HTTP_SHUTDOWN_TIMEOUT_MS`
- `MAX_CONCURRENT_REQUESTS`
- `MAX_CONCURRENT_PUBLISHES`
- `MAX_CONCURRENT_QUERIES`
- `CIRCUIT_FAILURE_THRESHOLD`
- `CIRCUIT_OPEN_MS`
- `CIRCUIT_HALF_OPEN_SUCCESSES`
- `PUBLISH_TIMEOUT_MS`
- `OUTBOX_LEASE_MS`
- `OUTBOX_BACKOFF_BASE_MS`
- `OUTBOX_BACKOFF_MAX_MS`
- `OUTBOX_BACKOFF_JITTER_PCT`
- `OUTBOX_QUARANTINE_AFTER`
- `CLEANUP_INTERVAL_MS`
- `CLEANUP_BATCH_SIZE`
- `IDEMPOTENCY_TTL_HOURS`
- `DEAD_LETTER_TTL_HOURS`
- `SUBJECT_RATE_LIMIT_RPS`
- `SUBJECT_RATE_LIMIT_BURST`
- `SUBJECT_RATE_LIMIT_RPS_OVERRIDES`
- `EVENT_SCHEMA_VERSION_RULES`
- `EVENT_SCHEMA_REQUIRED_FIELDS`
- `MODULE_START_RETRY_MAX`
- `MODULE_START_RETRY_BACKOFF_MS`

Limiter safety knobs:
- `RATE_LIMIT_MAX_IPS`
- `RATE_LIMIT_IP_TTL_SECONDS`

Additional safety knobs:
- `SECURITY_HEADERS_ENABLED=true`

## Local Run
```bash
docker compose -f deploy/docker-compose.yml up -d
go run ./cmd/controlplane
```

## Developer Commands
```bash
make test
make build
make run
make docker-build
```

## Publish Example
```bash
curl -X POST http://localhost:8080/events \
  -H "Authorization: Bearer replace-publish-token" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-A1001-v1" \
  -d '{"stream":"orders","type":"order.created","payload":{"orderId":"A-1001"}}'
```

## Kubernetes
K8s manifests live in `deploy/k8s` with deployment, service, autoscaling and ServiceMonitor.

## Visual Gallery
<p align="center">
  <img src="assets/images/internet-map.jpg" alt="Internet map" width="48%" />
  <img src="assets/images/data-center.jpg" alt="Data center" width="48%" />
</p>
