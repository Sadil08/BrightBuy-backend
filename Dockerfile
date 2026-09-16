# Multi-stage build (specs/global/12_DEVOPS_CICD.md §1.1):
#   Stage 1 has the full Go toolchain and compiles a static binary.
#   Stage 2 ships ONLY that binary — no compiler, no shell, no package manager, not even a
#   userland (distroless), running as a non-root user. A leaked container here has almost
#   nothing to pivot from.

# --- build stage ---
FROM golang:1.27-alpine AS builder
WORKDIR /src

# Copy just the dependency manifests first, so `go mod download` is cached by Docker as long as
# go.mod/go.sum haven't changed — editing application code won't invalidate this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: statically linked, no libc dependency, which is what makes running on
# distroless-static possible at all. -ldflags="-s -w" strips debug symbols to shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
