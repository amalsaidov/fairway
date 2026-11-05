# Fairway Deployment Guide

This guide covers deploying Fairway on any Docker host and on Railway, configuring runtime parameters, managing PostgreSQL securely, and verifying operational health.

---

## Architecture Overview

Fairway runs as a single Go binary or Docker container consisting of:
- **Scheduler**: Periodically executes Stellar Horizon strict-send pathfinding and queries Frankfurter mid-market FX rates across enabled corridors.
- **Store**: Persists measurements and state changes to PostgreSQL.
- **REST API**: Serves `/healthz`, `/corridors`, and `/corridors/{id}/history`.
- **Dispatcher**: Sends HTTP POST webhooks on corridor state changes (if configured).

Fairway requires network access to:
1. PostgreSQL (state persistence)
2. Stellar Horizon API (public default: `https://horizon.stellar.org`)
3. Frankfurter API (public default: `https://api.frankfurter.dev/v2/rates`)
4. Webhook endpoint (optional, if `WEBHOOK_URL` is set)

---

## Configuration Reference

Fairway reads configuration strictly from environment variables upon startup (defined in `internal/config/config.go`).

| Variable | Type | Default | Required | Description |
| :--- | :--- | :--- | :--- | :--- |
| `DATABASE_URL` | String | `""` (none) | **Yes** | PostgreSQL connection URI, e.g. `postgres://user:pass@host:5432/dbname?sslmode=disable`. |
| `HORIZON_URL` | String | `https://horizon.stellar.org` | No | Stellar Horizon node endpoint used for `/paths/strict-send` pathfinding queries. |
| `SELL_AMOUNT` | Float | `100.0` | No | Fallback sell asset amount when a corridor does not specify `target_usd_value`. |
| `MEASURE_INTERVAL` | Duration | `5m` | No | Frequency of measurement cycles as a Go duration string (e.g. `10m`, `15m`, `1h`). |
| `DEGRADED_LOSS_PCT`| Float | `2.5` | No | Loss percentage threshold ($\ge 2.5\%$) to classify corridor as `degraded`. |
| `UNUSABLE_LOSS_PCT`| Float | `5.0` | No | Loss percentage threshold ($\ge 5.0\%$) to classify corridor as `unusable`. |
| `WEBHOOK_URL` | String | `""` (disabled) | No | Destination URL for plain HTTP POST alerts on state change events. |
| `REFERENCE_RATE_URL` | String | `https://api.frankfurter.dev/v2/rates` | No | Frankfurter v2 rates endpoint for independent FX benchmarks. |
| `CORRIDORS_FILE` | String | `corridors.yaml` | No | Path to YAML file defining corridors to monitor and seed into PostgreSQL. |
| `LISTEN_ADDR` | String | `:8080` | No | TCP address and port for HTTP API server (`cmd/fairway/main.go`). |

---

## HTTP Port Configuration and Platforms with `$PORT`

Fairway binds its HTTP server using the `LISTEN_ADDR` variable (`cfg.ListenAddr` in `internal/config/config.go` and `cmd/fairway/main.go`).

Fairway does **not** automatically inspect the `PORT` environment variable. Cloud hosting platforms (such as Railway, Heroku, or Cloud Run) dynamically assign a port and inject it as `$PORT`.

On any platform that injects `PORT`:
- Explicitly set `LISTEN_ADDR` to bind to that injected port:
  ```bash
  LISTEN_ADDR=:${PORT}
  ```
- In platform dashboards supporting variable substitution (like Railway), configure:
  ```text
  LISTEN_ADDR=:${{PORT}}
  ```
  or set `LISTEN_ADDR` to `:8080` and instruct the platform to route traffic to port `8080`.

---

## Database Provisioning and Security

### 1. Private Network Requirement
**Never expose PostgreSQL publicly on the internet.**
- Always place PostgreSQL on the host's private network or an internal container network (e.g. Docker bridge, Railway private network `postgres.railway.internal`, or private VPC subnet).
- Only the Fairway container and administrative bastion tools should be able to reach port `5432`.
- In `docker-compose.yml`, note that PostgreSQL exposes port `5432` only to other containers on the Docker network; it does not map `5432:5432` to the host by default.

### 2. Manual Schema Initialization
Fairway does **not** run automatic schema migrations at runtime. Database tables and indexes must be applied **once by hand** against a fresh database before starting the application:

```bash
psql "$DATABASE_URL" -f schema.sql
```

This creates:
- `corridors` (seeded automatically by Fairway on startup from `corridors.yaml`)
- `measurements` (stores pathfinding results and FX rates)
- `state_change_events` (stores state transitions and webhook delivery status)
- Supporting indexes (`idx_measurements_corridor_time`, `idx_state_changes_corridor`)

---

## Deployment on Any Docker Host

### Using Docker Compose (Recommended for single-host VM)

1. Clone the repository and enter directory:
   ```bash
   git clone https://github.com/gofairway/fairway.git
   cd fairway
   ```

2. Inspect `docker-compose.yml`. For local development, `docker-compose.yml` mounts `schema.sql` into `/docker-entrypoint-initdb.d/01-schema.sql` so PostgreSQL applies it on first volume initialization. On a production host with an existing or remote Postgres instance, set `DATABASE_URL` in `.env`.

3. Create `.env` from `.env.example`:
   ```bash
   cp .env.example .env
   # Edit .env and adjust MEASURE_INTERVAL=10m
   ```

4. If connecting to a standalone Postgres container, apply `schema.sql`:
   ```bash
   psql "$DATABASE_URL" -f schema.sql
   ```

5. Build and launch containers:
   ```bash
   docker compose up -d
   ```

### Using Plain Docker

1. Build container image:
   ```bash
   docker build -t fairway:latest .
   ```

2. Run container connecting to your private database network:
   ```bash
   docker run -d \
     --name fairway-app \
     --restart unless-stopped \
     --network host_private_network \
     -e DATABASE_URL="postgres://fairway:secret@postgres-private:5432/fairway?sslmode=disable" \
     -e MEASURE_INTERVAL="10m" \
     -e LISTEN_ADDR=":8080" \
     -p 8080:8080 \
     fairway:latest
   ```

---

## Deployment on Railway

Railway provides managed PostgreSQL and container hosting within an isolated private network.

### Step 1: Create a Railway Project and Provision PostgreSQL
1. Create a new project in the [Railway Dashboard](https://railway.com/).
2. Add a **PostgreSQL** database service to the project.
3. Note the internal connection variable: `${{Postgres.DATABASE_URL}}` (uses `postgres.railway.internal`).

### Step 2: Initialize Database Schema
Apply `schema.sql` to the Railway database using the public connection string from your local terminal (used once for admin initialization):
```bash
psql "<RAILWAY_PUBLIC_POSTGRES_CONNECTION_URL>" -f schema.sql
```
Alternatively, use the Railway CLI:
```bash
railway run psql "$DATABASE_URL" -f schema.sql
```

### Step 3: Deploy Fairway Service
1. In the same Railway project, select **New Service** -> **GitHub Repo** (select `gofairway/fairway`) or deploy via Dockerfile.
2. In the service **Variables** tab, set:
   - `DATABASE_URL`: `${{Postgres.DATABASE_URL}}` *(uses private networking)*
   - `LISTEN_ADDR`: `:${{PORT}}` *(binds to Railway's injected port)*
   - `MEASURE_INTERVAL`: `10m` *(polite interval)*
   - `HORIZON_URL`: `https://horizon.stellar.org`
   - `REFERENCE_RATE_URL`: `https://api.frankfurter.dev/v2/rates`
   - `WEBHOOK_URL`: `https://your-webhook-endpoint.com/webhook` *(optional)*
3. In the service **Settings** tab:
   - **Healthcheck Path**: Set to `/healthz`
   - **Healthcheck Timeout**: 10 seconds

Railway will build the Dockerfile, link internal networking, monitor `/healthz`, and route inbound public traffic to the port bound by `LISTEN_ADDR`.

---

## Measurement Interval and Outbound API Requests

### Current Defaults
- **Default `MEASURE_INTERVAL`**: `5m` (5 minutes, defined in `internal/config/config.go`).
- **Configured Seed Corridors**: 3 corridors in `corridors.yaml` (`NGNC/USDC`, `NGNC/EURC`, `USDC/EURC`), all enabled.

### Request Count per Cycle
In each measurement cycle (`scheduler.measureAll`), the scheduler iterates sequentially over every enabled corridor (`measure.Measure`):

1. **Stellar Horizon (`/paths/strict-send`)**:
   - 1 HTTP request per corridor.
   - For 3 corridors: **3 requests per cycle**.

2. **Frankfurter Reference Rates (`/v2/rates`)**:
   - Dynamic trade sizing: If sell currency is not USD, calls `FetchRate(sellCurrency, "USD")`.
   - Reference rate benchmark: Calls `FetchRate(sellCurrency, buyCurrency)`.
   - Per corridor breakdown:
     - `NGNC/USDC`: 1 request (`NGN/USD` for sizing) + 1 request (`NGN/USD` benchmark) = **2 requests**.
     - `NGNC/EURC`: 1 request (`NGN/USD` for sizing) + 1 request (`NGN/EUR` benchmark) = **2 requests**.
     - `USDC/EURC`: 0 requests for sizing (sell currency is USD) + 1 request (`USD/EUR` benchmark) = **1 request**.
   - For 3 corridors: **5 requests per cycle**.

**Total outbound requests per cycle: 8 requests (3 Horizon + 5 Frankfurter).**

### Daily Request Volume at Default Interval (5m)
- 12 cycles/hour $\times$ 24 hours = 288 cycles/day.
- Horizon: $288 \times 3 =$ **864 requests/day** (36 requests/hour).
- Frankfurter: $288 \times 5 =$ **1,440 requests/day** (60 requests/hour).
- Total: **2,304 outbound requests/day**.

### Recommended Interval: $\ge$ 10 Minutes (`10m` or `15m`)
We strongly recommend setting `MEASURE_INTERVAL=10m` (or `15m` / `30m`) in production:
```bash
MEASURE_INTERVAL=10m
```
**Why:**
1. **Polite use of unauthenticated public APIs**: Frankfurter (`api.frankfurter.dev`) is a community-supported free utility that does not require an API key. Central bank FX fixing rates (e.g. European Central Bank and Central Bank of Nigeria) update once daily on working days. Polling every 5 minutes makes redundant queries for unchanging reference rates, increasing unnecessary load on public infrastructure and risking IP-based rate limiting (HTTP 429) or IP bans.
2. **Horizon rate limits**: Public Horizon nodes (`https://horizon.stellar.org`) enforce rate limits and are shared infrastructure for the entire Stellar developer ecosystem.
3. **Halving outbound volume**: At a 10-minute interval, total volume drops to 1,152 requests/day (432 Horizon, 720 Frankfurter), staying well within rate limits while providing prompt detection of payment corridor degradation.

---

## How to Read Logs

Fairway writes structured JSON logs to standard output (`os.Stdout`) via Go's standard library `log/slog`.

Key log events to monitor:
- `"database connected"`
- `"corridors seeded"` (`count: 3`)
- `"scheduler starting"` (`interval: 10m0s`)
- `"measured"` (`corridor: NGNC/USDC`, `state: unusable`, `path_found: true`, `loss_pct: ...`)
- `"state changed"` (`from: unknown`, `to: unusable`)

### Viewing Logs on Docker
```bash
docker logs -f fairway-app
```

### Viewing Logs on Docker Compose
```bash
docker compose logs -f app
```

### Viewing Logs on Railway
- In the Railway Dashboard: Open the service and select the **Deployments** -> **View Logs** tab.
- Via Railway CLI:
  ```bash
  railway logs
  ```

---

## Health Checks

Fairway provides a dedicated health check endpoint:
```http
GET /healthz
```
- Handled by `internal/api/api.go`.
- Executes `store.Ping(ctx)` against PostgreSQL.
- **200 OK**: Service is running and PostgreSQL is reachable (`{"status":"ok"}`).
- **503 Service Unavailable**: Database unreachable (`{"status":"error","detail":"db unreachable"}`).

Configure container orchestrators (Docker healthcheck, Kubernetes liveness/readiness probes, Railway healthcheck) to target `/healthz`.

---

## Verification ("Verify It Works")

Once deployed and running, verify the installation with these three checks:

### 1. Verify Health Endpoint
```bash
curl -i http://localhost:8080/healthz
```
Expected output:
```http
HTTP/1.1 200 OK
Content-Type: application/json
Content-Length: 16

{"status":"ok"}
```

### 2. Verify Configured Corridors
```bash
curl -i http://localhost:8080/corridors
```
Expected output:
```http
HTTP/1.1 200 OK
Content-Type: application/json

{"corridors":[{"ID":1,"Name":"NGNC/USDC",...},{"ID":2,"Name":"NGNC/EURC",...},{"ID":3,"Name":"USDC/EURC",...}]}
```

### 3. Verify Database Measurement Collection
Run a SQL query against PostgreSQL using `psql` to confirm measurements are being persisted:

Check total recorded measurements:
```bash
psql "$DATABASE_URL" -c "SELECT COUNT(*) FROM measurements;"
```

Inspect the latest recorded measurements with corridor names and integrity states:
```bash
psql "$DATABASE_URL" -c "
SELECT
    m.id,
    c.name AS corridor,
    m.measured_at,
    m.path_found,
    m.loss_pct,
    m.integrity_state
FROM measurements m
JOIN corridors c ON m.corridor_id = c.id
ORDER BY m.measured_at DESC
LIMIT 5;
"
```
If measurements are accumulating and `path_found` / `loss_pct` / `integrity_state` are populated, Fairway is fully operational.
