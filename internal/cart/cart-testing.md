How to run the BrightBuy backend locally and test the cart flow end to end.

This guide reflects the current implementation: the cart API is protected by the login cookie and
all cart routes are under `/api/v1` with the customer derived from the JWT claims, not from a
`?customer_id=` query string.

## Prerequisites

- Docker Desktop running
- Go installed
- Terminal opened in the backend folder: `cd "D:\DB project\BrightBuy-backend"`

## 1) Start the database

```powershell
docker compose up -d mysql
docker compose ps
```

The MySQL container should report `healthy` and expose port `3308`.

## 2) Run the app

```powershell
$env:DB_DSN = "brightbuy_app:devapppass@tcp(127.0.0.1:3308)/brightbuy?parseTime=true"
go run ./cmd/api

Then verify the app is up:
```powershell
curl.exe http://localhost:8080/healthz
curl.exe http://localhost:8080/readyz
```

## 3) Create a customer and log in

Use a real browser session or the PowerShell session cookie jar so the auth cookie is preserved.

```powershell
$base = "http://localhost:8080/api/v1"
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession

Invoke-RestMethod -Method Post "$base/auth/register" `
    -ContentType "application/json" `
    -Body '{"name":"Cart Tester","email":"cart.tester@example.com","password":"StrongPass123!","phone":"+15550000000"}' `
    -WebSession $session

Invoke-RestMethod -Method Post "$base/auth/login" `
    -ContentType "application/json" `
    -Body '{"email":"cart.tester@example.com","password":"StrongPass123!"}' `
    -WebSession $session
```

The login response sets the auth cookies, and subsequent requests on the same session can hit the
cart API without any customer ID query parameter.

## 4) Exercise the cart endpoints

```powershell
# Empty cart
Invoke-RestMethod -Method Get "$base/cart" -WebSession $session

# Add variant 1 with quantity 2
Invoke-RestMethod -Method Post "$base/cart/items" `
    -ContentType "application/json" `
    -Body '{"variantId":1,"quantity":2}' `
    -WebSession $session

# Add 3 more to the same line (the resulting quantity should be 5)
Invoke-RestMethod -Method Post "$base/cart/items" `
    -ContentType "application/json" `
    -Body '{"variantId":1,"quantity":3}' `
    -WebSession $session

# Update quantity for an existing cart item
$cart = Invoke-RestMethod -Method Get "$base/cart" -WebSession $session
$lineId = $cart.items[0].cartItemId
Invoke-RestMethod -Method Patch "$base/cart/items/$lineId" `
    -ContentType "application/json" `
    -Body '{"quantity":1}' `
    -WebSession $session

# Delete the item
Invoke-RestMethod -Method Delete "$base/cart/items/$lineId" -WebSession $session
```

## 5) Validation checks to try

These are the main behaviors the route tests cover:

- unauthenticated requests return `401`
- non-customer roles return `403`
- missing/invalid JSON body returns `400`
- quantity `0` or negative values are rejected
- quantity over `1000` is rejected
- unknown variant IDs return `404`
- quantity beyond available stock returns `400` with `STOCK_EXCEEDED`
- `PATCH` and `DELETE` require ownership of the cart item

Example validation request:

```powershell
try {
    Invoke-RestMethod -Method Post "$base/cart/items" `
        -ContentType "application/json" `
        -Body '{"variantId":9999,"quantity":1}' `
        -WebSession $session
} catch {
    $_.Exception.Response.StatusCode
    $_.ErrorDetails.Message
}
```

The current cart contract uses camelCase response fields such as `cartItemId`, `variantId`,
`stockWarning`, and `unavailable` exactly as defined in the OpenAPI schema.

### Expected results

| Test | Expected |
|---|---|
| Empty cart | No items, subtotal 0 |
| Add 2 | Quantity 2 |
| Add 3 more | Quantity 5 |
| Patch to 1 | Quantity 1 |
| Delete | Empty cart |
| Over stock / out of stock | 400, "exceeds available stock" |
| Quantity 0 | 400, "quantity must be at least 1" |
| Unknown variant | 400, "variant not found" |
| Update missing item | 400, "cart item not found" |

> Error statuses are all 400 for now. Planned: 404 for not found, 409 for stock conflicts.

---

## 6. Check the database directly

Run while the cart has items (for example after step 2 or 3).

```powershell
docker compose exec mysql mysql -ubrightbuy_app -pdevapppass brightbuy -e "SELECT * FROM cart; SELECT * FROM cart_item;"
```

---

## 7. Go tests

```powershell
# Everything.
go test ./...

# Only the cart stock test, verbose.
go test ./internal/cart/app -run TestService_AddItem_RejectsOverStock -v
```

The test connects to the real database, so MySQL must be running with migrations and seed data applied. Use a variant that exists and has low stock (for example variant `5`, stock 4), not an invented ID.

---

## Reset the local database

```powershell
# Deletes ALL local data (container volumes). Then redo steps 1 to 3.
docker compose down -v
```

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `docker` not recognized | Docker not running, or terminal opened before install | Start Docker Desktop, reopen the terminal |
| `no required module provides package .cmd/api` | Typo | Use `./cmd/api` |
| `Unable to connect to the remote server` | Server not running | Start it in Terminal 1 |
| `config load failed` | `DB_DSN` not set in this window | Set `$env:DB_DSN` again |
| `database unreachable` on `/readyz` | MySQL down or wrong port | `docker compose ps`, check port 3308 |
| `404 page not found` on `/cart` | Routes not registered | Check cart wiring in `cmd/api/main.go` |
| `Table ... doesn't exist` | Migration skipped | Rerun the migrations in order |
| `ERROR 1064` syntax error in a migration | Trailing comma before `)` | Remove the comma after the last line in the table |
| `ERROR 1050 Table already exists` | A previous run partly applied | Drop the cart tables and rerun |
| `Access denied` | Wrong user, password or port | Use the DSN above |
| Add shows Quantity 3 instead of 5 | Backend is still running the old replace-quantity behavior | Restart the server with the updated code |
| `invalid JSON` using `curl.exe -d` | PowerShell quote mangling | Use `Call-Api` or `-d "@body.json"` |
| Error body is empty in PowerShell | Response body already consumed | Use the `Call-Api` helper above |
| `mysql: [Warning] Using a password on the command line` | Expected for the dev DB | Ignore |
