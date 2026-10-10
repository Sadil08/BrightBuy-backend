# Deploying BrightBuy

BrightBuy is three moving parts, deployed in this order:

```
 MySQL 8  <--  backend (Go, container)  <--  frontend (Next.js, Vercel)  <--  browser
```

The browser only ever talks to the frontend. The frontend's server calls the backend, so the backend
needs no CORS setup and its address is never sent to the browser (`BACKEND_URL` has no
`NEXT_PUBLIC_` prefix on purpose).

## What CI/CD does today

| Repo | On every pull request | On every push to `main` |
|---|---|---|
| backend | gofmt, vet, staticcheck, gitleaks, unit and integration tests, compile, build image | the same, then **publish the image to `ghcr.io/<owner>/brightbuy-backend`** (`:latest` and `:<commit>`) |
| frontend | lint, typecheck, unit tests, production build, build image | the same, then publish `ghcr.io/<owner>/brightbuy-frontend`, then **deploy to Vercel** (once the secrets below exist) |

Nothing here needs a secret except the Vercel deploy. Until the three Vercel secrets exist, that job
prints a notice and passes, so CI stays green.

## 1. Database

Any managed MySQL 8 works (Aiven, Railway, TiDB Cloud, a VM...). Create a database named `brightbuy`
and **two** users, as `02_SECURITY_BASELINE.md` §6.1 requires:

- an **application** user with `SELECT, INSERT, UPDATE, DELETE, EXECUTE` on `brightbuy.*` (no DDL), used by the backend;
- a **migration** user with full rights on `brightbuy.*`, used only to run migrations.

Stored procedures and triggers need `log_bin_trust_function_creators=1` on hosts where binary
logging is on and your user has no SUPER privilege (error 1419 otherwise). Managed hosts expose this
in their advanced settings. Turn it on **before** running the migrations.

### Aiven specifics

Aiven gives you `defaultdb`, the `avnadmin` user, a non-3306 port, and **requires TLS** signed by
Aiven's own certificate authority. From the service overview, copy the host, port and user, and use
the **CA certificate** download button. Then, once, from the SQL console or `mysql` client as `avnadmin`:

```sql
CREATE DATABASE brightbuy CHARACTER SET utf8mb4;
CREATE USER 'brightbuy_app'@'%' IDENTIFIED BY '<a long random password>';
GRANT SELECT, INSERT, UPDATE, DELETE, EXECUTE ON brightbuy.* TO 'brightbuy_app'@'%';
```

Note the connection strings below do **not** contain `ssl-mode=...` (Aiven's own URI does): that is a
`mysql` client option, and this driver would send it to the server as a bogus session variable.
TLS is set up by `DB_CA_CERT` (backend) and `x-tls-ca` (migrate), described next.

## 2. Migrate

Run from a machine that can reach the database, as the migration user (`avnadmin` on Aiven).
`ca.pem` is the CA certificate downloaded from the host's console:

```bash
migrate -path db/migrations \
  -database 'mysql://avnadmin:PASSWORD@tcp(HOST:PORT)/brightbuy?multiStatements=true&x-tls-ca=/path/to/ca.pem' up
```

(For a database without a private CA, leave `x-tls-ca` out.) Re-run it on every release that adds a
file under `db/migrations/`. Checkout needs the rows migration `0010` seeds (tax rate and delivery
fee), so never skip migrations.

## 3. Backend

Run the published image on any container host. Environment variables:

| Variable | Value |
|---|---|
| `DB_DSN` | `brightbuy_app:PASSWORD@tcp(HOST:PORT)/brightbuy?parseTime=true` (the application user, never `avnadmin`) |
| `DB_CA_CERT` | only for a database with a private CA (Aiven): the **full text** of the CA certificate, `-----BEGIN CERTIFICATE-----` to `-----END CERTIFICATE-----`. The connection is then encrypted **and verified** against it |
| `JWT_SIGNING_KEY` | a random string of **at least 32 bytes**, from the host's secret store |
| `APP_ENV` | `production` (turns on `Secure` cookies) |
| `PORT` | usually injected by the host; the app listens on it (or on `ADDR`, which wins) |

Health checks: `GET /healthz` (process is up) and `GET /readyz` (database reachable).

Create the first administrator once, from the same image:

```bash
docker run --rm -e DB_DSN=... -e JWT_SIGNING_KEY=... ghcr.io/<owner>/brightbuy-backend:latest --create-first-admin
```

It prints a random password once and refuses to run if an admin already exists.

## 4. Frontend on Vercel

**Easiest: import the repo in Vercel.** vercel.com > Add New > Project > pick `BrightBuy-frontend`.
Framework is detected as Next.js. Under Environment Variables add:

| Variable | Value |
|---|---|
| `BACKEND_URL` | the backend's public URL, e.g. `https://brightbuy-api.example.com` (no trailing slash) |

Vercel then redeploys on every push to `main` by itself, and the CI job below is not needed.

**Or deploy from CI.** Run `npx vercel link` once locally, then in GitHub > Settings > Secrets and
variables > Actions add `VERCEL_TOKEN` (vercel.com/account/tokens), `VERCEL_ORG_ID` and
`VERCEL_PROJECT_ID` (both in `.vercel/project.json`). The `deploy-vercel` job activates on the next push.
Use one of the two ways, not both.

## Smoke test after a deploy

1. `GET <backend>/readyz` returns `ok`.
2. Open the Vercel URL: the home page shows categories and products.
3. Register, add something to the cart, check out with the *approved test card*: the order page
   shows the right totals and delivery date.
4. Payments are a stub in Phase 1 (`internal/payment`): no real card is ever charged.
