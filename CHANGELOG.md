# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - Unreleased

### Added
- Sequential measurement scheduler periodically monitoring enabled Stellar payment corridors (`internal/scheduler`).
- Stellar Horizon pathfinding client querying `/paths/strict-send` to evaluate execution routes and received amounts (`internal/measure`).
- Dynamic trade sizing dynamically scaling sell amounts from corridor `target_usd_value` using sell-currency USD exchange rates (`internal/measure`).
- Independent mid-market FX reference rate fetcher querying Frankfurter API v2 (`internal/reference`).
- Loss percentage computation benchmarking Horizon execution against mid-market FX rates (`internal/measure`).
- Three-tier corridor integrity classification (`usable`, `degraded`, `unusable`) based on configurable loss thresholds (`internal/measure`).
- Per-leg Stellar asset verification tracking anchor domain, metadata presence, and verified status independently per asset leg (`internal/store`, `corridors.yaml`).
- PostgreSQL persistence layer managing `corridors`, `measurements`, and `state_change_events` (`internal/store`, `schema.sql`).
- State-change detection comparing against preceding corridor measurements to prevent redundant notifications (`internal/scheduler`).
- Plain HTTP POST webhook dispatcher delivering alert payloads on corridor state transitions (`internal/webhook`).
- REST API server (`internal/api`) exposing:
  - `GET /healthz` health check with database ping probe.
  - `GET /corridors` listing enabled corridors and metadata.
  - `GET /corridors/{id}/history` returning paginated corridor measurement records.
- Automated startup seeding synchronizing corridor configurations from `corridors.yaml` into PostgreSQL (`internal/seed`).
- Multi-stage `Dockerfile` and `docker-compose.yml` configuration for containerized deployment.
- Environment variable configuration loader with defaults (`internal/config`).
