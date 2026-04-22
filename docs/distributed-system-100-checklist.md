# Distributed System 100-Point Checklist

Status key:
- `DONE`: implemented in current codebase baseline
- `GAP`: still missing and needs implementation

1. Request panic recovery middleware - `DONE`
2. Request ID generation and propagation - `DONE`
3. Request ID response header (`X-Request-Id`) - `DONE`
4. Structured audit correlation with request ID - `DONE`
5. Liveness endpoint (`/livez`) - `DONE`
6. Readiness endpoint (`/readyz`) - `DONE`
7. Detailed JSON readiness report (`/healthz`) - `DONE`
8. Per-module readiness latency reporting - `DONE`
9. HTTP read timeout - `DONE`
10. HTTP read-header timeout - `DONE`
11. HTTP write timeout - `DONE`
12. HTTP idle timeout - `DONE`
13. Configurable graceful shutdown timeout - `DONE`
14. Global request timeout middleware - `DONE`
15. Streaming endpoint timeout bypass (SSE safe) - `DONE`
16. Global request concurrency guard - `DONE`
17. Security headers middleware - `DONE`
18. Leader-only writes feature flag - `DONE`
19. Per-IP limiter bounded map size - `DONE`
20. Limiter TTL eviction - `DONE`
21. Oldest-entry eviction policy for limiter - `DONE`
22. Environment and K8s knobs for all above - `DONE`
23. Distinct startup probe endpoint - `DONE`
24. Dependency startup ordering + backoff strategy - `DONE`
25. Dependency circuit breaker (NATS) - `DONE`
26. Dependency circuit breaker (Cassandra) - `DONE`
27. Dependency circuit breaker (etcd) - `DONE`
28. Bulkhead isolation for publish pipeline - `DONE`
29. Bulkhead isolation for query pipeline - `DONE`
30. Outbox lease/claim ownership to avoid duplicate relay workers - `GAP`
31. Outbox poisoned-message quarantine queue - `GAP`
32. Outbox retry attempt counters persisted in DB - `GAP`
33. Outbox exponential backoff with jitter persisted by record - `GAP`
34. Publish timeout budget propagation - `GAP`
35. Per-subject publish quota/limits - `GAP`
36. Dead-letter replay endpoint - `GAP`
37. Dead-letter retention policy and cleanup job - `GAP`
38. End-to-end idempotency key TTL/expiration policy - `GAP`
39. Payload schema versioning and compatibility checks - `GAP`
40. Input JSON schema validation - `GAP`
41. Event type registry and unknown-type reject policy - `GAP`
42. Tenant isolation (tenant_id partitioning) - `GAP`
43. Tenant-aware auth scopes - `GAP`
44. Namespace-level quotas - `GAP`
45. API key rotation endpoint/process - `GAP`
46. Short-lived token support with exp/nbf checks - `GAP`
47. mTLS certificate rotation without restart - `GAP`
48. Secret manager integration (Vault/KMS) - `GAP`
49. Encrypt sensitive event payload fields at rest - `GAP`
50. PII redaction in audit logs - `GAP`
51. CORS policy controls - `GAP`
52. HSTS header support - `GAP`
53. Content-Security-Policy for any UI surface - `GAP`
54. API replay-attack nonce checks - `GAP`
55. Brute-force protection for token failures - `GAP`
56. Prometheus RED metrics per route - `GAP`
57. Per-dependency success/error/latency metrics - `GAP`
58. Queue depth metrics (outbox/inbox) - `GAP`
59. Leader election churn metric - `GAP`
60. Tracing baggage propagation across bus events - `GAP`
61. Trace sampling strategy by endpoint - `GAP`
62. Trace-to-log correlation IDs - `GAP`
63. OpenTelemetry metrics pipeline setup - `GAP`
64. Alert rules for SLO burn rates - `GAP`
65. Error budget policy docs - `GAP`
66. Multi-AZ failure-mode simulation tests - `GAP`
67. Chaos test harness (network partitions) - `GAP`
68. Jepsen-style consistency test suite - `GAP`
69. Fuzz tests for API payload decoding - `GAP`
70. Property-based tests for idempotency invariants - `GAP`
71. Load test profiles and threshold gates in CI - `GAP`
72. Race detector CI job on critical packages - `DONE`
73. Memory leak detection test harness - `GAP`
74. Canary rollout manifest set - `GAP`
75. Blue/green rollout manifests - `GAP`
76. Versioned API contract and deprecation policy - `GAP`
77. Backward compatibility matrix docs - `GAP`
78. Config validation with fail-fast invalid ranges - `GAP`
79. Dynamic config reload for runtime knobs - `GAP`
80. Feature flag framework for gradual releases - `GAP`
81. Controlled maintenance mode endpoint - `GAP`
82. Node drain mode for graceful traffic migration - `GAP`
83. Cluster membership introspection endpoint - `GAP`
84. Admin endpoint for outbox relay pause/resume - `GAP`
85. Clock skew detection and warnings - `GAP`
86. Monotonic sequence generation for event ordering - `GAP`
87. Duplicate delivery audit endpoint - `GAP`
88. Message reordering detector/metric - `GAP`
89. Exactly-once proof report tooling - `GAP`
90. Snapshot + restore automation - `GAP`
91. Disaster recovery runbook with RTO/RPO - `GAP`
92. Automated backup verification jobs - `GAP`
93. Data retention and compaction policy automation - `GAP`
94. Cost governance dashboard hooks - `GAP`
95. Dependency version drift monitoring - `GAP`
96. Supply-chain attestation (SLSA/Cosign) - `GAP`
97. SBOM generation in CI - `GAP`
98. CVE policy and patch automation - `GAP`
99. Contributor architecture decision record (ADR) templates - `GAP`
100. Production readiness checklist gate in CI - `DONE`
