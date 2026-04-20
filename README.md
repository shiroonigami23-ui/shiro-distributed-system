# Shiro Distributed System

A modular distributed-system foundation designed to combine proven OSS building blocks behind a single control plane.

## Vision
- Use strong existing tech instead of reinventing everything.
- Keep integration boundaries clean through module interfaces.
- Grow from a reliable core into production-ready services.

## Current Modules
- `natsbus`: event bus and pub/sub transport (NATS)
- `etcdcoord`: coordination, leases, leader election (etcd)
- `cassstore`: durable wide-column data layer (Cassandra)

## Quick Start
```bash
# from repo root
go run ./cmd/controlplane
```

Health endpoint:
- `GET http://localhost:8080/healthz`

## Local Infra (optional)
```bash
docker compose -f deploy/docker-compose.yml up -d
```

## Next Milestones
1. Add leader election and distributed lock API.
2. Add command/event log and replay.
3. Add sharding + replication policies.
4. Add observability (metrics, tracing, structured logs).
5. Add fault-injection and chaos test suite.
