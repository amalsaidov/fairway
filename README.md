# Fairway

[![CI](https://github.com/gofairway/fairway/actions/workflows/ci.yml/badge.svg)](https://github.com/gofairway/fairway/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gofairway/fairway)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Website:** https://gofairway.github.io

**Fairway is a continuous Stellar payment-corridor health monitor that measures real trade execution, tracks corridor health over time, and alerts when a corridor becomes unusable.**

## Problem

A Stellar payment corridor can look healthy in a one-off quote while being effectively unusable at the trade size that matters.

Fairway was built to answer a different question from a conventional quote or pathfinding tool:

> **Is this payment corridor actually usable right now, at a realistic economic trade size — and when did that change?**

The first measurements already show why continuous monitoring matters.

At a **$100-equivalent trade size**, Fairway measured:

| Corridor    | Observed loss | State       |
| ----------- | --------------: | ------------ |
| NGNC → USDC | **31.19%** | 🔴 Unusable |
| NGNC → EURC | **30.50%** | 🔴 Unusable |
| USDC → EURC | **-1.05%** | 🟢 Usable   |

In other words, **NGNC is currently unusable at the measured trade size on both tested corridors**, while the USDC/EURC corridor remains usable.

This is the gap Fairway fills: turning a one-time corridor measurement into a **continuous monitoring system with historical state and alerts**, so this kind of finding isn't a snapshot — it's something you'd actually know about the moment it happened.

It continuously asks:

* Is the corridor usable?
* Is it still usable at a meaningful economic trade size?
* How has its execution quality changed over time?
* When did its state change?
* What was the corridor's condition when the change occurred?

That makes Fairway a monitoring layer rather than a one-shot quote tool.

---

## What it does

Fairway currently provides:

* **Scheduled measurement** — periodically prices configured Stellar payment corridors using Horizon pathfinding.
* **Economic trade sizing** — measures corridors using USD-equivalent trade sizes instead of arbitrary asset-unit amounts.
* **Independent FX benchmarking** — uses Frankfurter reference rates to make measured loss meaningful across currencies.
* **Per-leg asset verification** — independently verifies both the asset being sold and the asset being bought.
* **Historical storage** — persists every measurement in PostgreSQL for later inspection and analysis.
* **State tracking** — detects transitions such as usable → unusable rather than treating every measurement as an isolated event.
* **Webhook alerts** — sends a webhook when a corridor changes state.
* **REST API** — exposes corridor configuration, current state, and historical measurements.
* **Containerized deployment** — includes Docker and Docker Compose for local or server deployment.
* **Automated testing and CI** — protects measurement, persistence, and state-transition behavior.

---

## Quickstart

### Requirements

The easiest way to run Fairway locally is with Docker Compose.

You will need:

* Docker
* Docker Compose

Clone the repository:

```bash
git clone https://github.com/gofairway/fairway.git
cd fairway
```

Start Fairway:

```bash
docker compose up
```

This starts the Fairway service together with its PostgreSQL dependency. Docker Compose applies schema.sql automatically on first start. With your own Postgres, apply it once with psql "$DATABASE_URL" -f schema.sql (see docs/deployment.md).

Once the services are running, the API can be checked with:

```bash
curl http://localhost:8080/healthz
```

### Running in the background

```bash
docker compose up -d
docker compose logs -f
docker compose down
```

### Configuration

Fairway's runtime configuration is supplied through environment variables — see `.env.example` for the full list and defaults. At minimum, a deployment needs access to:

* PostgreSQL
* Stellar Horizon
* Frankfurter for reference FX rates
* A webhook destination, if state-change notifications are enabled

For local development, Docker Compose provides the recommended starting point.

---

## Corridor data

Fairway currently seeds corridors involving three assets:

### NGNC

**Issuer**
```text
GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6
```
**Domain**
```text
ngnc.online
```
**SEP-1 status**
```text
live
```

NGNC has complete SEP-1 metadata and a declared home domain.

Despite this metadata being available, Fairway's real-trade measurements show severe execution loss at the tested economic size:

* **NGNC → USDC: 31.19% loss**
* **NGNC → EURC: 30.50% loss**

The important distinction is that **asset metadata and corridor usability are different questions**. An asset can be discoverable and correctly identified while the actual payment route remains economically unusable.

### USDC

**Issuer**
```text
GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN
```
**Domain**
```text
centre.io
```

USDC was independently verified through substantial on-chain payment activity and an `attestation_of_reserve`. The asset does not expose an explicit `status` field in the same way as NGNC, so Fairway does not manufacture one.

### EURC

**Issuer**
```text
GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2
```

EURC presents a different verification signal. It has:

* **525,977+ payments**
* No `home_domain`
* No available `stellar.toml`

Fairway therefore flags the missing issuer transparency information as a **verification risk**, rather than silently treating the asset as fully verified. This distinction is intentional: **missing metadata is itself useful monitoring information**.

### Current corridor measurements

| Corridor    | Trade size | Measured loss | Current state |
| ----------- | --------------: | ------------: | ---------------------- |
| NGNC → USDC | $100 equivalent | 31.19% | Unusable |
| NGNC → EURC | $100 equivalent | 30.50% | Unusable |
| USDC → EURC | $100 equivalent | -1.05% | Usable |

These measurements are not intended to be universal or permanent claims about Stellar liquidity. They are observations made using Fairway's measurement methodology at a specific economic trade size and time. As Fairway continues running, the historical dataset becomes the more important artifact.

### Verification methodology

The complete methodology for asset verification, issuer metadata, trade sizing, pathfinding, FX benchmarking, and interpretation of measurements is documented in:

**[`docs/corridor-verification.md`](docs/corridor-verification.md)**

---

## How measurement works

Fairway deliberately avoids measuring corridors using arbitrary unit quantities. Measuring a corridor with 100 NGNC does not necessarily represent a meaningful economic trade — if 100 NGNC is worth only a few cents, the resulting path quality tells us very little about how the corridor behaves for an economically meaningful payment.

Fairway instead determines a **USD-equivalent trade size** and constructs the measurement around that value.

```text
Configured corridor
       │
       ▼
Per-leg asset verification
       │
       ▼
USD-equivalent trade size
       │
       ▼
Stellar Horizon pathfinding
       │
       ▼
Observed corridor output
       │
       ▼
Independent FX benchmark
       │
       ▼
Loss calculation
       │
       ▼
State classification
       │
       ▼
PostgreSQL measurement
       │
       ▼
State-change detection
       │
       ▼
Webhook notification
```

---

## Architecture

```text
fairway/
├── cmd/
│   └── fairway/            # Application entrypoint; wires everything together
│
├── internal/
│   ├── config/              # Env var + corridors.yaml loading
│   ├── seed/                # Idempotent corridor config -> Postgres upsert
│   ├── measure/              # Horizon pricing, loss %, state classification (ScoreState)
│   ├── reference/            # Frankfurter FX reference-rate fetcher
│   ├── store/                 # PostgreSQL persistence + per-leg verification resolution
│   ├── webhook/               # State-change webhook dispatcher
│   ├── scheduler/             # Sequential scheduled measurement loop
│   └── api/                   # REST API (healthz, corridors, corridor history)
│
├── docs/
│   └── corridor-verification.md
│
├── schema.sql               # Postgres DDL, applied directly (no migration tool)
├── Dockerfile
├── docker-compose.yml
├── corridors.yaml           # Seed corridor configuration
├── .env.example
└── README.md
```

### Measurement

Determines what a real trade would receive through Stellar pathfinding, independent of persistence or notification concerns.

### Store

Persists every individual measurement, not just the latest value — making historical questions possible: when did a corridor deteriorate, how long was it unusable, did it recover, how often does it cross the threshold.

### Verification

Verification is per-leg. For a corridor `Asset A → Asset B`, Fairway independently evaluates Asset A and Asset B. A corridor should not be considered fully trustworthy simply because one side is well documented — its effective trust is only as strong as its weakest leg.

### Scheduler

Runs measurements sequentially, by design. A previous race condition (fixed and regression-tested) demonstrated that concurrent measurements could produce incorrect, duplicate state-change events — sequential execution keeps state transitions deterministic.

### Webhooks

Fairway alerts on **state changes**, not every measurement. A transition from `usable` to `unusable` fires a webhook; repeated `unusable` readings do not re-trigger it. This keeps the webhook a useful operational signal rather than a stream of raw measurements.

---

## API

```http
GET /healthz
```
Used by Docker, orchestration systems, and operators to confirm the service is running.

```http
GET /corridors
```
Returns configured corridors with their per-leg verification status.

```http
GET /corridors/{id}/history
```
Returns historical measurements for a specific corridor — Fairway's core value isn't just the current reading, it's knowing how a corridor's condition changed over time. Supports limit (default 50, max 200) and offset query parameters.

---

## Data model

```text
Corridor
├── sell asset (code, issuer, domain, anchor_metadata, verified_status)
├── buy asset  (code, issuer, domain, anchor_metadata, verified_status)
└── measurements
      ├── timestamp
      ├── trade size (USD-equivalent + computed sell_amount)
      ├── received_amount
      ├── reference_rate + reference_src
      ├── loss_pct
      └── integrity_state (usable / degraded / unusable)
```

State transitions are recorded in a separate state_change_events table (corridor, from and to state, triggering measurement, and webhook delivery status).

---

## Design principles

### Measure economically meaningful trades
A corridor should be evaluated against a trade size that represents a meaningful payment, not a quantity that happens to be convenient for an API request.

### Verify both sides of the corridor
A payment route depends on both the source and destination assets — verification is a per-leg concern.

### Separate market observation from external benchmarks
Horizon pathfinding provides the observed route; Frankfurter provides an independent FX reference. Keeping these separate prevents the measurement from defining its own notion of value.

### Preserve history
The latest measurement alone is insufficient for monitoring. Every measurement contributes to a record that can reveal deterioration, recovery, instability, and persistent illiquidity.

### Alert on state changes
Notify operators when something changes, not repeatedly confirm that nothing has.

### Do not overclaim what the measurement proves
Fairway measures **economic execution quality and corridor usability**. It does not prove that a market will always execute at the measured price, that an asset is safe merely because its metadata verifies, that a corridor is permanently usable or unusable, or that an issuer is trustworthy solely from on-chain metadata. Fairway reports observable signals and preserves the evidence needed to evaluate them.

---

## Project status

### Working
* Stellar asset verification (per-leg)
* Horizon pathfinding measurements
* USD-equivalent trade sizing
* Independent FX benchmarking
* PostgreSQL persistence
* Corridor state tracking with regression-tested state-change detection
* Sequential scheduling
* Webhook state-change notifications
* REST API
* Seed corridor measurements
* Automated test coverage (unit + regression, fully isolated from live network calls)
* CI (GitHub Actions: vet, build, race-detector test run)
* Dockerfile + Docker Compose (verified end-to-end)

### In progress
* Expanded corridor coverage
* API refinement
* Longer-running historical monitoring data

---

## Contributing

Fairway is intended to be useful both as a monitoring service and as an open-source observability project for the Stellar ecosystem.

Good areas for contribution include: additional corridor verification, measurement reliability, database and historical analysis, state-classification logic, API improvements, webhook integrations (e.g. Discord/Slack-formatted payloads), tests, documentation, and additional Stellar assets and corridors.

Before adding a new corridor, please follow the methodology described in [`docs/corridor-verification.md`](docs/corridor-verification.md).

---

## Why Fairway?

Stellar already has excellent tools for discovering assets, inspecting protocols, observing contracts, and finding payment paths. Fairway focuses on what happens **after discovery**:

> **Can this corridor actually carry a meaningful payment right now, and will we know when that changes?**

A snapshot can tell you what a corridor looks like once. A monitoring system can tell you whether it stayed usable, when it deteriorated, and when it recovered. That's the problem Fairway is built to solve.
