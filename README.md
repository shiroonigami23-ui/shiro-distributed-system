# Shiro Distributed System

<p align="center">
  <img src="assets/images/cloud-computing-layers.png" alt="Cloud Layers" width="720" />
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white"></a>
  <img alt="Architecture" src="https://img.shields.io/badge/Architecture-Modular-0A66C2?style=for-the-badge&logo=dependabot&logoColor=white">
  <img alt="Messaging" src="https://img.shields.io/badge/NATS-JetStream-27AE60?style=for-the-badge">
  <img alt="Coordination" src="https://img.shields.io/badge/etcd-Leader%20Election-34495E?style=for-the-badge">
  <img alt="Storage" src="https://img.shields.io/badge/Cassandra-Durable%20Events-E67E22?style=for-the-badge">
  <img alt="API" src="https://img.shields.io/badge/API-REST%20%2B%20SSE-8E44AD?style=for-the-badge">
</p>

A high-performance distributed-system control plane that combines proven best-in-class patterns:
- NATS for event streaming and pub/sub
- etcd for coordination and leader election
- Cassandra for durable event storage

## What You Get
- Real connection and readiness checks
- Leader election with observable leader state
- Event publish and live stream consume
- Durable event append and query
- Clean module interfaces for extension

## API Endpoints
- `GET /healthz` readiness status
- `GET /leaderz` current leader and node leadership
- `POST /events` persist + publish event
- `GET /events?stream=<name>&limit=<n>` list recent events
- `GET /stream?subject=events.>` live event stream (SSE)

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
