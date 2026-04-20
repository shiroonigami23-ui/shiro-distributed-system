# Shiro Distributed System

<p align="center">
  <img src="assets/images/cloud-computing-layers.png" alt="Cloud Layers" width="720" />
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white"></a>
  <img alt="Architecture" src="https://img.shields.io/badge/Architecture-Modular-0A66C2?style=for-the-badge&logo=dependabot&logoColor=white">
  <img alt="Messaging" src="https://img.shields.io/badge/NATS-JetStream-27AE60?style=for-the-badge">
  <img alt="Coordination" src="https://img.shields.io/badge/etcd-Leader%20Election-34495E?style=for-the-badge">
  <img alt="Storage" src="https://img.shields.io/badge/Cassandra-Outbox%2FInbox-E67E22?style=for-the-badge">
  <img alt="API" src="https://img.shields.io/badge/API-REST%20%2B%20SSE-8E44AD?style=for-the-badge">
</p>

A distributed-system control plane with production-first patterns built in.

## Core Features
- Real NATS + JetStream event transport
- Real etcd leader election
- Real Cassandra durable event store
- Exactly-once/idempotent publish flow (idempotency key + outbox state)
- Inbox de-dup for consumers
- API auth + ACL enforcement
- Shared mTLS support across module connections
- Prometheus metrics endpoint `/metrics`
- Kubernetes deployment + HPA + ServiceMonitor manifests

## API Endpoints
- `GET /healthz` readiness status
- `GET /metrics` Prometheus metrics
- `GET /leaderz` current leader and node leadership (admin scope)
- `POST /events` persist + publish event with idempotency (rw scope)
- `GET /events?stream=<name>&limit=<n>` list recent events (rw scope)
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

## Local Run
```bash
docker compose -f deploy/docker-compose.yml up -d
go run ./cmd/controlplane
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

## Image Credits
These images are from Wikimedia Commons and should follow their original licenses:
- Cloud computing layers: https://commons.wikimedia.org/wiki/File:Cloud_computing_layers.png
- Data center: https://commons.wikimedia.org/wiki/File:Data_Center.jpg
- Internet map: https://commons.wikimedia.org/wiki/File:Internet_map_1024.jpg
