# BrightBuy Study Guide

A running notebook for everything learned while building BrightBuy: Go, databases,
web dev, CI/CD, security. Add to it as we go — see "How to use this doc" below.

---

## How to use this doc

- New sections get appended under the right topic heading (Go / DB / Web / CI-CD / Security).
- Big one-off explanations (like this first one) go under **Part 1: Project Tour**, tied to
  actual files in the repo.
- General concepts that show up *in* this project but apply everywhere go under
  **Part 2: Concepts**, grouped by subject.
- Keep a dated one-line entry in the **Log** at the bottom every time we cover something new —
  it's the changelog of your own learning.

---

# Part 1: Project Tour — what's actually in this repo

## `cmd/api/main.go` — the entry point

Every Go program starts at a `func main()` inside `package main`. This is that file for the
whole backend. Run it with `go run ./cmd/api`.

It's deliberately called the **"composition root"** (see the file's top comment): the *only*
place in the codebase allowed to know about every concrete, real implementation (real MySQL
repository, real HTTP handler, etc). Everywhere else in the code only knows about *interfaces*
(abstract contracts) — main.go is where the real objects get built and wired together. This is
the **Dependency Injection** pattern, done by hand (no framework needed in Go).

What it does, in order:
1. **Load config** (`config.Load()`) — reads environment variables. Fails immediately (`os.Exit(1)`)
   if something required is missing. This is "fail fast": better to crash at startup with a clear
   message than three requests into production with a nil pointer.
2. **Set up logging** (`logging.New(cfg.Env)`).
3. **Open the database pool** (`dbx.Open(cfg.DatabaseDSN)`) — also fails fast if MySQL isn't
   reachable. `defer db.Close()` schedules cleanup for when `main()` returns.
4. **Build the router**: [chi](https://github.com/go-chi/chi) is a lightweight HTTP router/mux for
   Go's standard `net/http`. Three middlewares are attached:
   - `RequestID` — tags every request with a correlation ID (for tracing a request through logs).
   - `Logger` — logs method/path/status/latency per request.
   - `Recoverer` — turns a panic into an HTTP 500 instead of crashing the whole process.
5. **Health endpoints**:
   - `/healthz` (liveness) — "is the process alive at all?" Always returns 200. Checks nothing else
     on purpose — a container orchestrator (Kubernetes, ECS, etc.) uses this to decide whether to
     kill and restart the process.
   - `/readyz` (readiness) — "can this instance serve traffic right now?" Pings the database. The
     orchestrator uses this to decide whether to *route requests* to this instance.
   - This liveness/readiness split is a standard pattern in containerized deployments.
6. **Start the server in a goroutine** (`go func() { srv.ListenAndServe() ... }()`) — a goroutine is
   a lightweight concurrent function. Running the server in one frees the main goroutine to wait
   for a shutdown signal next.
7. **Graceful shutdown**: blocks on `<-stop` until it receives SIGINT (Ctrl+C) or SIGTERM (what
   Docker/Kubernetes send before killing a container during a deploy). Then calls `srv.Shutdown(ctx)`,
   which stops accepting *new* connections but lets in-flight requests finish, up to a 10s timeout.
   This is what makes a deploy "zero-downtime" instead of dropping mid-request users.

Nothing is registered yet under "Feature routes" — that's where each feature (catalog, cart,
checkout...) will plug in its handler once built.

## `internal/` — Go's "private to this module" folder

Any package under a Go module's `internal/` directory can only be imported by code *inside* that
same module tree. It's a language-enforced way of saying "this is implementation detail, not a
public library" — nothing outside `brightbuy-backend` could import `brightbuy-backend/internal/...`
even if it wanted to.

Inside `internal/`, this repo is organized **by feature**, not by technical layer:
```
internal/
  catalog/        <- one feature ("browse products")
    domain/       <- pure business types, no framework/DB/HTTP dependencies
    app/          <- service/use-case layer, orchestrates domain + repository
    mysql/        <- the repository: talks to MySQL, implements domain interfaces
  shared/         <- cross-cutting code every feature is allowed to depend on
```
This is **Clean/Hexagonal Architecture**-flavored layering: `domain` knows nothing about SQL or
HTTP; `mysql` implements a repository interface that `domain`/`app` defined; `app` contains the
actual business logic (e.g. `catalog_service.go`); a future `http` package per feature would expose
it over the wire. Dependencies point inward (mysql → domain), never outward (domain never imports
mysql) — that's what keeps business logic testable without a real database.

## `internal/shared/` — cross-cutting infrastructure

Code every feature module is allowed to depend on, because it's infrastructure, not business logic.

### `shared/config` — environment configuration
`config.go` reads all settings from environment variables (`os.Getenv`) exactly once, in one place,
and returns a plain `Config` struct. Rule enforced here: **no other package in the codebase calls
`os.Getenv` directly** — everything else receives config explicitly as a parameter. This makes it
obvious where every setting comes from, and makes testing easy (just construct a `Config` struct,
no environment needed).

### `shared/dbx` — database connection + transaction helper
Wraps Go's standard `database/sql` package for MySQL specifically.
- `Open(dsn)`: creates a **connection pool** (`*sql.DB`), *not* a single connection — the pool
  opens/reuses TCP connections to MySQL as needed. Bounds it with `SetMaxOpenConns(10)` /
  `SetMaxIdleConns(10)` / `SetConnMaxLifetime(5min)`, then calls `PingContext` to prove the DB is
  actually reachable at startup (opening a pool alone doesn't connect to anything yet).
- `WithTx(ctx, db, fn)`: the **only** function in the codebase allowed to call `db.BeginTx`. Runs
  `fn` inside `BEGIN` ... `COMMIT`/`ROLLBACK`, so every feature's multi-step writes (e.g. "reduce
  stock and create an order row") are atomic through one shared, tested code path instead of each
  feature reimplementing transaction handling.

### `shared/httpx` — HTTP response helpers
Defines the **one JSON error shape** the whole API ever returns:
```json
{"code": "STOCK_EXCEEDED", "message": "human readable text", "fields": [...]}
```
`WriteJSON` / `WriteError` are the only two ways any handler writes a response, so a client always
sees the same shape regardless of which feature/handler produced it — no raw Go error strings or
stack traces ever get leaked to a client (a security consideration, not just consistency).

### `shared/logging` — structured logging setup
Go 1.21+ has structured logging in the standard library: `log/slog`. "Structured" means every log
line carries key/value fields, not just a free-text sentence — a log aggregator (Datadog, CloudWatch,
etc.) can filter/query on those fields instead of regex-grepping strings.
`logging.New(env)` picks the output format: JSON in `production`/`staging` (machine-parseable),
human-readable text in local dev. This is the *only* place that decision is made — every other
package just calls `slog.Info(...)` without knowing where the bytes end up.

### `shared/money` — exact-precision currency type
Go has no built-in "money" type, and `float64` cannot represent decimal fractions like `0.10`
exactly in binary — summing many prices silently accumulates rounding error. `Money` instead stores
an amount as an **integer count of cents** (`cents int64`); integers add/subtract with zero rounding
error. Highlights:
- `Parse("19.99")` / `String()` convert between the decimal text form and the internal integer form.
- Implements several standard Go interfaces so it "just works" everywhere a normal type would:
  - `json.Marshaler`/`Unmarshaler` — serializes as the *string* `"19.99"` in JSON, never a JSON
    number (a JSON number becomes a float in JS/Python/etc. on the other end — exactly the bug this
    type exists to avoid).
  - `sql.Scanner`/`driver.Valuer` — lets a repository do `row.Scan(&variant.Price)` directly against
    a MySQL `DECIMAL(10,2)` column, and lets it be used directly as a query argument on writes.
- Because it's used by `domain` packages, it imports **only the standard library** — no DB or HTTP
  dependency, by the same "domain must stay pure" rule described above.

## `db/` — schema and seed data (not Go code)

### `db/migrations/*.up.sql` / `*.down.sql`
**Migrations** are version-controlled, incremental changes to the database schema, applied by a
tool ([golang-migrate](https://github.com/golang-migrate/migrate), listed in `go.mod`). Each
numbered pair is one change:
- `*.up.sql` — applies the change (e.g. `0001_catalog_schema.up.sql` creates `category`, `product`,
  `product_variant`, attribute tables, indexes, foreign keys).
- `*.down.sql` — reverses it, so a bad migration can be rolled back instead of hand-fixing the DB.

Why not just hand-edit the database? Migrations mean every environment (your laptop, CI's
disposable test container, staging, production) can be brought to the exact same schema state by
replaying the same ordered list of files — the schema is versioned in git just like the code that
depends on it.

Things worth noticing in `0001_catalog_schema.up.sql`:
- `DECIMAL(10,2)` for `price`, never `FLOAT`/`DOUBLE` — same reasoning as the `Money` type above.
- `CHECK (price >= 0)` — a database-level constraint, a second line of defense even if application
  code has a bug.
- `FOREIGN KEY ... ON DELETE CASCADE` — deleting a `product` automatically deletes its
  `product_variant` rows, keeping the DB from ever holding an orphaned variant.
- `FULLTEXT INDEX` on `product(name, description)` — a MySQL index type built for natural-language
  search (`MATCH ... AGAINST`), not just exact/prefix lookups.
- `UNIQUE KEY` on `sku`/`name` — enforced at the DB layer, not just checked in app code.

### `db/seed/catalog.sql`
Sample/starter data (rows) to `INSERT` after migrations run, so local dev and tests have realistic
data to work against without a human typing it in by hand.

## `.github/workflows/ci.yml` — the CI pipeline
Runs on every push/PR to `main` via GitHub Actions. Ordered **cheapest/fastest checks first** so a
trivial mistake (a missing `gofmt`) fails in seconds instead of waiting on a slow integration test
suite: `gofmt` → `go vet` → `staticcheck` → `gitleaks` (secret scanning) → unit tests → integration
tests (spun up against a real, disposable MySQL via `testcontainers-go`, no manual `services:`
container needed) → `go build`. A separate `build-image` job only runs on `main` and only after
`lint-and-test` passes (`needs:`), building the Docker image.

---

# Part 2: Concepts (general — apply beyond this repo)

## Go language

| Concept | Where seen | Idea |
|---|---|---|
| Packages & modules | every file | A Go **module** (`go.mod`, `module brightbuy-backend`) is the whole project/repo; a **package** is a folder of `.go` files sharing a namespace. |
| `internal/` | folder layout | Compiler-enforced privacy: only importable from within the same module. |
| Interfaces (implicit) | `domain` ↔ `mysql` | A type satisfies an interface just by having the right methods — no `implements` keyword. Lets `app`/`domain` depend on an interface while `mysql` provides the real implementation. |
| Struct + methods, value vs pointer receiver | `money.Money` | `func (m Money) String()` (value receiver, read-only) vs `func (m *Money) Scan(...)` (pointer receiver, mutates the caller's value in place). Rule of thumb: mutate → pointer receiver. |
| Error handling | everywhere | Go has no exceptions/`try-catch`. Functions return `(value, error)`; caller checks `if err != nil`. `fmt.Errorf("...: %w", err)` **wraps** an error so the original is still inspectable (`errors.Is`/`errors.As`) while adding context. |
| `defer` | `main.go`, `dbx.Open` | Schedules a call to run when the *current function* returns, regardless of how it returns (normal return, panic, early `return`). Used for guaranteed cleanup (`defer db.Close()`). |
| Goroutines | `main.go` | `go func() {...}()` starts a function running concurrently (a lightweight thread managed by the Go runtime, not the OS). |
| Channels | `main.go`'s `stop` | A typed pipe for communicating between goroutines; `signal.Notify(stop, ...)` + `<-stop` blocks until a value arrives. |
| `context.Context` | `dbx`, `main.go` readiness handler | Carries cancellation/deadlines through a call chain. `context.WithTimeout(ctx, 2*time.Second)` makes anything using that context give up after 2s. |
| Closures | `handleReadiness(db)` | A function that "closes over" a variable from its enclosing scope (`db`) and returns a new function (`http.HandlerFunc`) that still has access to it — avoids needing a package-level global. |
| Standard library only, by convention | `money` package | A team rule (not the compiler) that certain packages may only import the stdlib, to guarantee they stay free of framework/DB dependencies. |

## Databases & data management

| Concept | Where seen | Idea |
|---|---|---|
| Connection pool | `dbx.Open` | `*sql.DB` isn't one connection — it's a managed pool, reused across requests. Pool size is a real capacity-planning knob (`SetMaxOpenConns`). |
| DSN (Data Source Name) | `config.DatabaseDSN` | The connection string format a specific driver understands (host, user, password, DB name, options). |
| Migrations (up/down) | `db/migrations` | Schema changes as ordered, versioned, reversible files — the DB equivalent of git commits for structure. |
| Transactions (BEGIN/COMMIT/ROLLBACK) | `dbx.WithTx` | A group of writes that either all succeed or all get undone — needed whenever two related rows must change together (e.g. decrement stock *and* create an order). |
| Constraints (`CHECK`, `UNIQUE`, `FOREIGN KEY ... CASCADE`) | migrations | Data-integrity rules enforced by the database itself, as a backstop below the application layer. |
| `DECIMAL` vs `FLOAT` for money | migrations, `money.go` | Binary floats can't represent every decimal fraction exactly; fixed-point/integer-cents avoids silent rounding drift. |
| Fulltext index | migrations | A MySQL index type purpose-built for natural-language text search, distinct from a normal B-tree index. |
| Seed data | `db/seed` | Data inserted after migrations so dev/test environments have something realistic to run against. |
| Disposable test databases (testcontainers) | `ci.yml`, `product_repository_integration_test.go` | Integration tests spin up a real, throwaway MySQL in Docker per test run instead of mocking the database — catches real SQL-dialect bugs that a mock would hide. |

## Web development

| Concept | Where seen | Idea |
|---|---|---|
| Router/mux | `chi.NewRouter()` | Maps an HTTP method+path to a handler function. |
| Middleware | `r.Use(...)` | A function that wraps a handler to add cross-cutting behavior (logging, request IDs, panic recovery) without every handler repeating that code. |
| Liveness vs readiness probes | `/healthz` vs `/readyz` | Liveness = "restart me if this fails"; readiness = "don't route to me if this fails." Different failure responses call for different orchestrator actions. |
| Consistent API error shape | `httpx.ErrorResponse` | Every endpoint returns errors in one predictable JSON shape, so clients write one error-handling code path instead of one per endpoint. |
| Graceful shutdown | `srv.Shutdown(ctx)` | Stop accepting new connections but let in-flight ones finish, within a deadline — avoids cutting off users mid-request during a deploy. |
| Server timeouts | `ReadTimeout`/`WriteTimeout`/`ReadHeaderTimeout` | Guard against slow/stalled clients holding a connection open indefinitely and exhausting server resources. |

## Security

| Concept | Where seen | Idea |
|---|---|---|
| Config/secrets from environment, never source | `config.go` | Credentials (`DB_DSN`, `JWT_SIGNING_KEY`) are never hardcoded or committed — they're injected at runtime via env vars. |
| Fail fast on missing required config | `config.Load()` | Refusing to start with an unclear error is safer than starting in a half-broken state and failing mysteriously later. |
| Spoofable headers | `main.go` comment on `RealIP` | `X-Forwarded-For`/`X-Real-IP` are client-supplied and can be faked unless a trusted proxy strips/re-sets them — trusting them blindly (e.g. for rate-limiting by IP) is a bypassable control. |
| No raw errors/stack traces to clients | `httpx` | Internal error details can leak implementation info useful to an attacker; only a controlled `code`/`message` shape goes out. |
| Panic recovery | `middleware.Recoverer` | An unhandled panic in one request shouldn't crash the whole process and take down every other in-flight request. |
| Secret scanning in CI | `ci.yml` (gitleaks) | Automated check that no credential/key was accidentally committed, run on every push/PR. |

## CI/CD

| Concept | Where seen | Idea |
|---|---|---|
| Pipeline staging (cheap → expensive) | `ci.yml` | Fast checks (format/vet) run before slow ones (integration tests, image build) so failures are caught quickly. |
| Static analysis (`go vet`, `staticcheck`) | `ci.yml` | Catches bugs/style issues without running the code. |
| Unit vs integration tests | `ci.yml`, `-tags=integration` | Unit tests are fast and isolated (no real DB); integration tests exercise real infrastructure (real MySQL via testcontainers) and are slower — Go build tags (`-tags=integration`) let one command run only one kind. |
| Job dependencies (`needs:`) | `ci.yml` | `build-image` only runs after `lint-and-test` succeeds — don't build/ship an artifact from code that didn't pass checks. |
| Branch-gated jobs | `ci.yml` (`if: github.ref == 'refs/heads/main'`) | Some steps (like building/publishing an image) should only happen on the main branch, not every PR. |

---

# Log

A one-line dated entry per session/topic covered — append, don't rewrite history.

- **2026-09-30** — First pass: toured `cmd/api/main.go`, `internal/` layout, `internal/shared/*`
  (config, dbx, httpx, logging, money), `db/migrations` + `db/seed`, and `.github/workflows/ci.yml`.
  Created this study guide.
