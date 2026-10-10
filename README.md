# BrightBuy — Backend

BrightBuy is a retail inventory and online order management system for an electronics-and-toys
retailer in Texas (University of Moratuwa DBMS module, Group 11). This repository is the **Go REST
API** over **MySQL 8**. It is one of three repositories:

| Repository | What it holds |
|---|---|
| `brightbuy-backend` (this one) | Go API, SQL migrations, seed data |
| `brightbuy-frontend` | Next.js storefront + staff console |
| `specs` | The spec system: SRS-traceable specs, plans, tasks and `openapi.yaml` (the API contract) |

> The specs are the source of truth. Every feature follows **spec → plan → tasks → code → tests**
> (`../specs/README.md`). Where code and spec disagree, the spec or the code is changed in the same
> pull request — never left to drift.

## What it does

| # | Feature | SRS | Highlights |
|---|---|---|---|
| 01 | Catalogue | REQ-1 | Browse, full-text search, category filter, ETag-cached product detail with images |
| 02 | Auth & RBAC | REQ-2 | Register/login, JWT in HttpOnly cookies, refresh-token rotation with reuse detection, runtime-editable roles and permissions |
| 03 | Cart | REQ-3 | Server cart for customers, guest cart merge |
| 04 | Checkout & orders | REQ-4, 7 | `sp_place_order` (atomic, idempotent), tax/fee from config, COD or card (stub processor) |
| 05 | Delivery estimation | REQ-6 | Main/Other city rule, +3 days when out of stock, preview equals checkout |
| 06 | Inventory | REQ-5 | `sp_adjust_stock`, full `stock_movement` audit trail |
| 07 | Admin catalogue | REQ-10 | Product/variant/category management, deactivate-never-delete, presigned-URL image upload |
| 08 | Order status | REQ-8 | Guarded status transitions (trigger + service), cancel returns stock, history, COD → Paid on delivery |
| 09 | Reporting | REQ-9 | Five SQL views behind `reports:view`, JSON or CSV, read-only DB credential |

## Architecture in one minute

```
cmd/api/main.go            composition root: reads config, wires every module by hand, starts the server
internal/<module>/
    domain/                entities and rules; imports nothing but the standard library
    app/                   use-cases (services) and the ports they need
    mysql/                 hand-written SQL implementing those ports  (no ORM, by decision)
    httpapi/               chi handlers, DTOs, route registration
internal/shared/           auth (JWT, RBAC middleware), config, db pool + transactions, http helpers, money
db/migrations/             golang-migrate versioned SQL, one up/down pair per change
db/seed/                   deterministic sample data
db/deploy/                 privileged, non-migration steps (e.g. the reporting DB user's grants)
```

Business rules that must never be bypassed (stock never negative, valid order transitions, audit rows)
live **in the database** (procedures, triggers, constraints) as well as in Go, so a bug in one layer
cannot corrupt data.

## Prerequisites

- Go (version in `go.mod`)
- Docker (MySQL 8 and MinIO run in containers; integration tests start their own with testcontainers)
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI (`migrate`)

## Run it locally

```bash
# 1. start MySQL and MinIO (S3-compatible image storage)
docker network create brightbuy-shared        # once, lets the frontend stack reach this one
docker compose up -d mysql minio

# 2. apply the schema (use the root DB user: migrations create procedures and triggers)
migrate -path db/migrations \
  -database "mysql://root:devrootpass@tcp(127.0.0.1:3307)/brightbuy?multiStatements=true" up

# 3. optional: sample data (40+ products, cities, checkout config)
for f in catalog identity delivery checkout; do
  docker compose exec -T mysql mysql -uroot -pdevrootpass brightbuy < db/seed/$f.sql
done

# 4. optional: read-only reporting user (spec 09, SEC-REPORTING-1)
docker compose exec -T mysql mysql -uroot -pdevrootpass < db/deploy/0013_reporting_grants.sql

# 5. run the API
cp .env.example .env && export $(grep -v '^#' .env | xargs)
go run ./cmd/api

# 6. create the first administrator (prints a one-time password; refuses if an ADMIN already exists)
go run ./cmd/api --create-first-admin
```

Check it: `curl localhost:8080/healthz` and `curl localhost:8080/readyz` (the latter also pings MySQL).

Or run everything in containers: `docker compose up --build`.

> MySQL is published on host port **3307** (not 3306) so it can't clash with a MySQL already running
> on your machine. Inside the Docker network it is still `mysql:3306`.

### Environment variables

| Variable | Required | Meaning |
|---|---|---|
| `DB_DSN` | yes | `user:pass@tcp(host:port)/brightbuy?parseTime=true` (the least-privilege app user) |
| `JWT_SIGNING_KEY` | yes | ≥ 32 bytes; signs access and refresh tokens |
| `ADDR` / `PORT` | no | listen address (default `:8080`) |
| `APP_ENV` | no | `local`, `staging`, `production` (log format only) |
| `DB_CA_CERT` | no | PEM of the CA for a managed MySQL with a private CA (enables verified TLS) |
| `REPORTING_DB_DSN` | no | read-only `brightbuy_reporting` credential; reporting uses the main pool if unset |
| `S3_ENDPOINT_URL`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET` | no | object store for product images; image routes are only mounted when the endpoint is set |
| `S3_PUBLIC_URL` | no | public base URL images are served from (default `<endpoint>/<bucket>`) |

## Database migrations

Migrations are plain SQL in `db/migrations`, applied in numeric order by `golang-migrate`, each with a
`.down.sql` that reverses it.

```bash
migrate -path db/migrations -database "$MIGRATE_DSN" up        # apply everything
migrate -path db/migrations -database "$MIGRATE_DSN" down 1    # undo the latest
migrate -path db/migrations -database "$MIGRATE_DSN" version   # current version
migrate -path db/migrations -database "$MIGRATE_DSN" force N   # clear a "dirty" flag after a failed run
```

| Version | Purpose |
|---|---|
| 0001–0003 | Catalogue schema, `fn_is_variant_in_stock`, variant `updated_at` |
| 0004 | Identity: users, roles, **permissions**, role→permission seed |
| 0005 | Cart schema |
| 0006 | `sp_adjust_stock` |
| 0007–0010 | Checkout schema, `sp_place_order`, `fn_estimate_delivery_days`, config seed |
| 0011 | `product_image` |
| 0012 | Order-status triggers and `sp_cancel_order` |
| 0013 | The five reporting views |

Version numbers must be unique: two files with the same number make `golang-migrate` refuse to run.

## Tests and quality gates

```bash
go test ./... -short                      # unit tests (no Docker)
go test ./... -tags=integration           # + integration tests (testcontainers starts MySQL, applies real migrations)
go vet ./... && gofmt -l .                # must print nothing
```

Integration tests assert what the **database** does (a trigger rejecting an illegal transition, a
procedure returning stock), not just that an endpoint answers 200. Reporting's integration test also
checks the read-only user *cannot* read the `order` table.

## CI/CD (`.github/workflows/ci.yml`)

On every pull request: `gofmt` → `go vet` → `staticcheck` → `gitleaks` (secret scan) → unit tests →
integration tests → build → Docker image build. On pushes to `main` the image is also published to
GHCR. Cheap checks run first so a trivial mistake fails in seconds. See `docs/DEPLOYMENT.md` for the
hosted deployment (managed MySQL, container backend, Vercel frontend) and
`../specs/global/12_DEVOPS_CICD.md` for the reasoning.

## API

The contract is `../specs/openapi/openapi.yaml`. Errors always have one shape:
`{"code": "...", "message": "..."}`. Money is a decimal **string** (`"49.99"`), never a float.

Roles: `CUSTOMER`, `WAREHOUSE_STAFF`, `ORDER_MANAGER`, `MANAGER`, `ADMIN`. Handlers check
**permissions** (`catalog:write`, `stock:adjust`, `order:status:update`, `order:cancel`,
`reports:view`, …), never role names, so an admin can re-shape roles at runtime.

## Known limitations (deliberate, documented)

- Card payments use a stub processor (`internal/payment`); the port is real, the provider is not.
- Customer-initiated cancellation/returns are out of scope for Phase 1 (SRS TBD-4).
- Variant stock cannot be edited through the catalogue API — stock changes go through inventory so
  each one is audited.
- Image upload needs a reachable S3-compatible store (MinIO locally).

## Team and contributions

| Member | Contribution |
|---|---|
| **Sadil** | Project lead and integrator. Wrote the spec system and OpenAPI contract; repository skeleton and CI/CD; **01 Catalogue** and the `Money` type; **02 Auth & RBAC** (JWT, refresh rotation, dynamic permissions); checkout configuration, city listing and migration fixes; DB TLS and deployment work; merged and reconciled features 07–09 (migration renumbering, schema-correct reporting views, shared auth wiring, staff order queue, contract alignment) and built the frontend consoles for them |
| **Raveen** (`yasankharaveen-web`) | **03 Cart** (domain, SQL repository, service, endpoints, guest merge) and **04 Checkout & orders** backend (order persistence, payment stub, customer sessions, checkout service), plus the guest/customer cart experience in the frontend |
| **Isuru** (`imISURUB`) | **05 Delivery estimation**, **06 Inventory stock**, the catalogue schema/stock function/seed data, frontend→backend health proxy |
| **Navod** (`Navod Thiekshana`) | **07 Admin catalogue** (CRUD, image pipeline) and **08 Order tracking & status** (transition validation, cancellation, staff handlers) |
| **Pathuman** (`pathuman2004-ui`) | **09 Management reporting** (five views, repository, service, handlers, CSV, tests) |

Commit history keeps every author's original commits; `git log --format='%an' | sort | uniq -c` shows it.
