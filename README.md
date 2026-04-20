# Shiro Distributed System

A modular distributed-system control plane that integrates proven capabilities from NATS, etcd, and Cassandra.

## Features (Phase 2)
- Real NATS connection and JetStream-backed publishing
- Real etcd leader election
- Real Cassandra durable event storage
- Health and readiness checks across all modules
- HTTP APIs for leader status, event publish/list, and live stream

## Endpoints
- `GET /healthz`: module readiness
- `GET /leaderz`: current leader and local leadership status
- `POST /events`: persist event in Cassandra and publish via NATS
- `GET /events?stream=<name>&limit=<n>`: list recent events from Cassandra
- `GET /stream?subject=events.>`: live NATS subscription via SSE

## Environment Variables
- `HTTP_ADDR` (default `:8080`)
- `NODE_ID` (default hostname)
- `NATS_URL` (default `nats://localhost:4222`)
- `NATS_STREAM` (default `SHIRO_EVENTS`)
- `ETCD_ENDPOINTS` (default `localhost:2379`)
- `LEADER_ELECTION_KEY` (default `/shirods/controlplane/leader`)
- `CASSANDRA_HOSTS` (default `localhost:9042`)
- `CASSANDRA_KEYSPACE` (default `shirods`)

## Quick Start
```bash
docker compose -f deploy/docker-compose.yml up -d
go run ./cmd/controlplane
```

## Publish Example
```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{"stream":"orders","type":"order.created","payload":{"orderId":"A-1001"}}'
```
