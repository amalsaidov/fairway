# Security Audit and Hardening Review

**Date:** 2026-11-06  
**Auditor:** Fairway Engineering  
**Scope:** Repository-wide security review and hardening across `cmd/fairway`, `internal/api`, `internal/config`, `Dockerfile`, `.dockerignore`, `.gitignore`, `.github/workflows`, and repository root files.  
**Constraint:** Measurement behaviors in `internal/measure`, `internal/scheduler`, `internal/store`, `internal/reference`, and `internal/webhook` preserved intact.

---

## Executive Summary

A comprehensive security audit of Fairway was conducted covering secret detection, dependency vulnerabilities, database query safety, API input validation, HTTP server timeouts, security headers, CORS policies, in-memory rate limiting, container isolation, and CI/CD workflow permissions.

All discovered issues were fixed and verified through automated test suites (`go test -race ./...`), static analysis (`go vet`, `golangci-lint`), vulnerability scanning (`govulncheck`), and secret scanning (`gitleaks`).

---

## 1. Secrets Management & Environment Configuration

### Audit Findings
- No hardcoded API keys, private tokens, or production credentials exist in the source code, repository configuration, Dockerfile, or workflow definitions.
- Local development database credentials in `docker-compose.yml` and CI workflows utilize non-production test defaults (`fairway:fairway`).
- `.env` was previously ignored in `.gitignore`, but secondary environment variants (`.env.*`, `.env.local`) were unlisted.

### Actions Taken
- Hardened `.gitignore` to comprehensively ignore all `.env` variants (`.env`, `.env.*`, `.env.local`), editor folders (`.idea/`, `.vscode/`), and editor artifacts (`*.swp`, `*.bak`, `*~`, `.DS_Store`).
- Verified that logs do not print database connection credentials or webhook target URLs.

---

## 2. Git History Leak Scan

### Audit Findings
- Running `gitleaks detect --verbose` across all 56 historical commits reported a single finding:
  - **Commit:** `5354ec4b9211b5430c364b933a0cebbb86ed8cb5`
  - **File:** `internal/reference/reference.go:62`
  - **Match:** `DefaultFrankfurterV2URL`
  - **Rule:** `generic-api-key`
- **Analysis:** This finding was manually inspected and determined to be a false positive. The pattern triggered on the constant identifier `DefaultFrankfurterV2URL` within a Go doc comment.
- A manual `git log -p` search for credential keywords (`api_key`, `token`, `password`, `bearer`, `private_key`) across repository history confirmed that no private credentials, keys, or secrets were ever committed.

### Actions Taken
- Created `.gitleaksignore` documenting the false positive fingerprint (`5354ec4b9211b5430c364b933a0cebbb86ed8cb5:internal/reference/reference.go:generic-api-key:62`).
- Re-ran `gitleaks detect --verbose`, confirming clean exit with zero leaks found.

---

## 3. Dependency & Vulnerability Analysis

### Audit Findings
- Running `go list -m -u all` and `go mod tidy` confirmed Fairway relies on only two direct external dependencies:
  - `github.com/lib/pq v1.12.3` (PostgreSQL driver)
  - `gopkg.in/yaml.v3 v3.0.1` (YAML parser)
- Running `govulncheck ./...` confirmed:
  - **Zero** vulnerabilities in imported application packages (`github.com/lib/pq`, `gopkg.in/yaml.v3`).
  - Standard library vulnerability reports flagged by `govulncheck` are tied to the local host Go compiler version and are addressed by updating the build environment runtime.

### Actions Taken
- Ran `go mod tidy` to clean module directives and promote direct package imports.
- Preserved existing major dependency versions. Zero new third-party packages were introduced for security features; all hardening was implemented using the Go standard library.

---

## 4. API Endpoints & Database Query Safety

### Audit Findings
- **Database Layer:** Audited all SQL statements in `internal/store/store.go`. Every query utilizes parameterized placeholders (`$1`, `$2`, etc.) via `database/sql`. Zero dynamic string concatenation or query formatting exists.
- **Input Validation:**
  - Route path parameter `{id}` was previously parsed with `strconv.Atoi` but did not reject non-positive IDs ($\le 0$) or excessively large integers.
  - Query parameters `limit` and `offset` lacked validation for negative, malformed, or excessively large values.
- **HTTP Methods:**
  - Non-GET/HEAD HTTP methods did not return structured JSON error payloads with standard `Allow` headers.
  - Endpoint errors previously utilized `http.Error`, resulting in inconsistent `text/plain` responses alongside JSON payloads.

### Actions Taken
- Updated `handleCorridorHistory`:
  - Enforced strict `{id}` validation ($id \ge 1$ and $id \le 1,000,000,000$). Non-numeric, zero, negative, or out-of-range values return `400 Bad Request` with `{"error": "invalid corridor id"}`.
  - Enforced `limit` validation: values $\le 0$ or non-numeric return `400 Bad Request` with `{"error": "limit must be greater than 0"}`. Valid values $> 200$ are safely capped at `200`.
  - Enforced `offset` validation: values $< 0$ or non-numeric return `400 Bad Request` with `{"error": "offset must be greater than or equal to 0"}`. Excessively large offsets ($> 1,000,000$) return `400 Bad Request` with `{"error": "offset is too large"}`.
- Replaced all raw `http.Error` invocations with a standardized `writeError` helper ensuring `Content-Type: application/json` is returned on all error conditions.
- Implemented method enforcement returning `405 Method Not Allowed` with `Allow: GET, HEAD, OPTIONS` and `{"error": "method not allowed"}` for all non-GET/HEAD/OPTIONS requests.
- Routed unmatched paths to a standardized `404 Not Found` JSON handler returning `{"error": "not found"}`.

---

## 5. HTTP Server Hardening

### Audit Findings
- `cmd/fairway/main.go` configured `ReadTimeout`, `WriteTimeout`, and `IdleTimeout`, but omitted `ReadHeaderTimeout` and `MaxHeaderBytes`.
- Omitting `ReadHeaderTimeout` leaves HTTP servers susceptible to Slowloris resource exhaustion where attackers trickle request headers.
- Omitting `MaxHeaderBytes` permits unbounded request header buffers.

### Actions Taken
- Configured explicit timeouts on `http.Server`:
  - `ReadHeaderTimeout: 5 * time.Second` (mitigates Slowloris header trickling).
  - `ReadTimeout: 15 * time.Second` (bounds total request read duration).
  - `WriteTimeout: 30 * time.Second` (bounds total response transmission duration).
  - `IdleTimeout: 60 * time.Second` (bounds keep-alive connection reuse).
  - `MaxHeaderBytes: 1 << 20` (1 MiB maximum request header size).
- Preserved graceful server shutdown using `httpServer.Shutdown` with a 10-second timeout context.

---

## 6. Security Headers

### Audit Findings
- The API previously emitted no security headers.

### Actions Taken
- Implemented global middleware setting defense-in-depth headers on every HTTP response:
  - `X-Content-Type-Options: nosniff` (disables MIME sniffing).
  - `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` (disallows scripts, plugins, frames, and styling, suited for a JSON-only API).
  - `Referrer-Policy: no-referrer` (prevents leaking query parameters or endpoints in Referer headers).
  - `X-Frame-Options: DENY` (disallows framing/clickjacking).
  - `Cache-Control`:
    - `no-store, no-cache, must-revalidate` on `/healthz` to prevent caching health status.
    - `no-cache, must-revalidate` on data endpoints (`/corridors`, `/corridors/{id}/history`) ensuring clients always receive current corridor health measurements.

---

## 7. Cross-Origin Resource Sharing (CORS)

### Audit Findings
- Prior implementation emitted no CORS headers, blocking cross-origin browser fetches (such as frontend dashboards hosted on `https://gofairway.github.io`).
- Wildcard CORS (`Access-Control-Allow-Origin: *`) should never be set accidentally.

### Actions Taken
- **Safe Default Decision:** By default, CORS is **disabled** (`CORS_ALLOWED_ORIGINS=""`). No `Access-Control-Allow-Origin` header is emitted unless an administrator explicitly configures allowed origins.
- Added environment variable `CORS_ALLOWED_ORIGINS`:
  - Supports an explicit comma-separated allowlist of origins (e.g. `https://gofairway.github.io, https://app.example.com`).
  - Request `Origin` headers are checked against the allowlist. Only matching origins receive `Access-Control-Allow-Origin: <origin>` and `Vary: Origin`.
  - Supports explicit wildcard `*` only if deliberately configured by the operator.
  - Responds to `OPTIONS` preflight requests with `204 No Content` and allowed method/header directives.

---

## 8. In-Memory Rate Limiting

### Audit Findings
- The API previously had no rate limiting, leaving it vulnerable to request flooding and denial of service.

### Actions Taken
- Built an in-memory token bucket rate limiter in `internal/api/limiter.go` using only the Go standard library (`sync.Mutex`, `time.Time`, `net`).
- **Configuration (via environment variables with safe defaults):**
  - `RATE_LIMIT_ENABLED` (default: `true`)
  - `RATE_LIMIT_RPS` (default: `10.0` tokens/second per client IP)
  - `RATE_LIMIT_BURST` (default: `30` tokens maximum capacity per client IP)
  - `TRUST_PROXY` (default: `false`)
- **Proxy Trust Model & Trade-Off:**
  - When `TRUST_PROXY=false` (default), client IP resolution strictly uses `r.RemoteAddr`. Spoofed `X-Forwarded-For` headers are ignored, protecting directly internet-facing deployments.
  - When `TRUST_PROXY=true`, the limiter inspects the leftmost IP in `X-Forwarded-For`.
  - *Trade-off documented:* When running behind a reverse proxy (e.g. Railway, Cloudflare, AWS ALB) with `TRUST_PROXY=false`, all clients share the proxy's IP rate limit bucket. Enabling `TRUST_PROXY=true` restores per-user throttling behind a proxy, but must only be enabled when a trusted proxy strips untrusted client headers.
- **Throttled Responses:** Requests exceeding limits receive `429 Too Many Requests`, a `Retry-After: 1` header, and `{"error": "rate limit exceeded"}`.
- **Memory Management:** A background cleanup goroutine automatically evicts idle IP buckets older than 3 minutes, preventing unbounded memory growth.

---

## 9. Debug Endpoints & Route Exposure

### Audit Findings
- Searched codebase for `pprof`, `expvar`, `/debug`, and administrative routes.
- **Result:** No profiling, debugging, or admin endpoints are registered. The only routes exposed are:
  - `GET /healthz`
  - `GET /corridors`
  - `GET /corridors/{id}/history`

---

## 10. Repository File Audit

### Audit Findings
- Inspected all 42 tracked repository files (`git ls-files`).
- Confirmed zero accidental files: no editor backups (`.bak`, `.swp`, `*~`), no OS metadata (`.DS_Store`), no local `.env` files, no SSH/TLS private keys, and no build artifacts.

---

## 11. Container Security

### Audit Findings
- `Dockerfile` previously executed the container process as `root`.
- `.dockerignore` was missing from the repository, potentially leaking local files into Docker build contexts.

### Actions Taken
- Updated `Dockerfile` runtime stage:
  - Created unprivileged group and user: `RUN addgroup -S fairway && adduser -S fairway -G fairway`.
  - Switched runtime execution: `USER fairway`.
  - Kept CA certificates (`ca-certificates tzdata`) in both builder and runtime stages for secure outbound HTTPS to Horizon and Frankfurter.
- Created `.dockerignore` explicitly excluding `.git`, `.github`, `.env*`, test artifacts, documentation, and tooling configs from the build context.

---

## 12. CI/CD Workflow Hardening

### Audit Findings
- `.github/workflows/ci.yml` lacked a top-level `permissions:` block, defaulting to permissive repository token defaults.
- `.github/workflows/docker.yml` defined top-level `packages: write` across all triggers including untrusted pull requests.

### Actions Taken
- Added `permissions: contents: read` to `ci.yml`.
- Restricted top-level permissions in `docker.yml` to `permissions: contents: read`, scoping `packages: write` exclusively to the build job.
- Confirmed all GitHub Actions are pinned to major releases (`@v4`, `@v5`, `@v6`).
- Confirmed no secrets are exposed to pull requests from fork repositories.

---

## 13. Authentication, Access Control & Architecture

### Audit Findings & Verification
- Fairway is architected as an unauthenticated, public, read-only monitoring API by design.
- Verified:
  - No administrative or mutating endpoints exist.
  - No HTML or user-submitted template rendering exists (eliminates XSS vectors).
  - All responses consistently set `Content-Type: application/json`.
  - Sensitive internal database errors and stack traces are logged server-side only and never leaked to API clients.

---

## 14. Known Limitations & Threat Model

1. **Single-Instance In-Memory Rate Limiting:**
   - The token bucket rate limiter maintains state in memory per application process. In multi-instance or horizontal autoscaling deployments, rate limit buckets are not shared across replicas. Distributed rate limiting (e.g. via Redis) is intentionally omitted to avoid introducing external infrastructure dependencies.
2. **Reverse Proxy IP Trust:**
   - Client IP detection relies on `TRUST_PROXY`. If misconfigured on a host without an upstream reverse proxy, clients can manipulate `X-Forwarded-For` to evade rate limiting.
3. **Public Read-Only Access:**
   - Because Fairway does not require API keys or user authentication, denial-of-service mitigation relies on edge reverse proxies, CDN caching, and the in-memory rate limiter.
