# cluster-sampleapp

A minimal spec-driven Go service. It exposes a single HTTP `GET /` endpoint that
echoes every request header back as plain text, plus the timestamp the database
was first bootstrapped:

```
Accept: */*
User-Agent: curl/8.4.0
X-Custom-Header: hello-world

Sample App Bootstrapped At: 2026-06-28T20:29:21.123456Z
```

## How it works

- **Spec-driven server.** The HTTP server interface is generated from
  [`api/openapi.yaml`](api/openapi.yaml) with
  [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) (chi server).
  Regenerate with `make generate`; never edit `api/server.gen.go` by hand.
- **Postgres.** Connects via `pgx` (through the `database/sql` adapter).
- **Migrations.** [`rubenv/sql-migrate`](https://github.com/rubenv/sql-migrate)
  runs the embedded SQL in [`data/migrations`](data/migrations) on startup. The
  single migration creates a `sample` table (`id UUID`, `created_at TIMESTAMPTZ`)
  and seeds one row with `NOW()` — that row's timestamp is the "bootstrapped at"
  value.

## Configuration

| Env var        | Purpose                                                              |
| -------------- | ------------------------------------------------------------------- |
| `PG_PASSWORD`  | Password for the in-cluster DSN (production).                       |
| `DATABASE_URL` | Full connection string; overrides the default DSN (local / tests).  |
| `PORT`         | HTTP listen port (default `8080`).                                  |

In production the app connects to:

```
postgresql://app:$PG_PASSWORD@cnpg-cluster-rw.databases.svc.cluster.local:5432/app
```

## Develop

```sh
make generate   # regenerate the server from the spec
make build      # build the binary
make test       # run tests (spins up a Postgres container via Docker)
make run        # run locally (set DATABASE_URL first)
```

### Run locally against a throwaway Postgres

```sh
docker run --rm -d --name pg -e POSTGRES_USER=app -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=app -p 5432:5432 postgres:16-alpine
DATABASE_URL='postgresql://app:secret@localhost:5432/app?sslmode=disable' make run
curl -s localhost:8080/ -H 'X-Custom-Header: hello-world'
```

## Tests

`internal/handler/handler_test.go` starts a real Postgres
([testcontainers-go](https://golang.testcontainers.org/)), runs the migrations,
and asserts the endpoint echoes headers and returns a valid RFC3339 UTC
bootstrap timestamp. Requires a running Docker daemon.
