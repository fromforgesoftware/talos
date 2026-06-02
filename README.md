# talos

Forge telemetry + audit service: ingests and streams audit events (gRPC + REST),
append-only partitioned storage, live tail/subscribe. Named for the bronze
sentinel that watched the shores.

Ships an audit-sink adapter (`talos/audit`) implementing `go-kit/audit.Sink`, so
other services (e.g. aegis) forward audit events without importing talos.
