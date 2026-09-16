# brightbuy-backend

The Go backend for BrightBuy (Retail Inventory and Online Order Management System). See
`../specs/` (in the parent `BrightBuy` folder — that's the single source of truth this codebase
follows: `../specs/README.md` for the spec system, `../specs/global/11_BUILD_PLAN.md` for what's
built and what's next).

## Local development

**Option A — Docker Compose (recommended, matches production topology):**
```bash
docker network create brightbuy-shared   # once, ever — lets brightbuy-frontend reach this stack
docker compose up --build
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

**Option B — run the Go binary directly against a Dockerized MySQL:**
```bash
docker compose up mysql minio -d
cp .env.example .env   # then edit if needed
export $(cat .env | xargs)
go run ./cmd/api
```

## Checks (what CI runs — `.github/workflows/ci.yml`)
```bash
gofmt -l .          # formatting
go vet ./...         # correctness lints
staticcheck ./...    # deeper static analysis
go test ./... -short # unit tests
go build ./...        # compiles
```

## Layout

See `../specs/global/01_TECH_STACK.md` §2 and `../specs/global/06_ENGINEERING_STANDARDS.md` for
why the code is organized this way (clean architecture, package-by-module, manual dependency
injection wired in `cmd/api/main.go`).

```
cmd/api/main.go       composition root — wires everything together, starts the server
internal/shared/       cross-cutting: config, logging, db pool + transactions, HTTP response helpers
internal/<module>/     added one per feature — domain/app/mysql/httpapi layers each
db/migrations/          golang-migrate versioned SQL, one pair of files per schema change
db/seed/                 deterministic sample-data script
```
