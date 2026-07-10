# cluster-sampleapp

A spec-driven Go service that demonstrates a small user-lifecycle domain over RabbitMQ, exercising
all three exchange types (direct / topic / fanout) plus Postgres persistence and an HTTP read model.
One image bakes three binaries; each is deployed as its own workload.

## Three services, one image

| Binary (`cmd/…`) | Workload | Role |
| --- | --- | --- |
| `/manager` (default) | sample-user-manager | HTTP (`GET /users`) + Postgres. Consumes `create-user-command`; per command it persists the user, emits `users.created` + an audit message, and — once there are more than 10 users — deletes the oldest and emits `users.deleted` + an audit message. |
| `/signup` | sample-user-signup | Emits `create-user-command` every 10s (`{uuid, timestamp}`). Consumes `users.created` and every audit message, logging both. |
| `/auditor` | sample-audit-logger | Consumes **only** audit messages, logging each to stdout. |

### Topology — one of each exchange type

| Exchange | Type | Owner | Published | Consumed by |
| --- | --- | --- | --- | --- |
| `create-user-command` | **direct** (command, N→1) | manager | signup | manager |
| `user-events` | **topic** (event, 1→N, filtered) | manager | `users.created`, `users.deleted` | signup binds **only** `users.created`; `users.deleted` has no consumer by design |
| `user-audit-logger` | **fanout** (broadcast, 1→all) | manager | every create/delete | signup **and** auditor |

This is the teaching point: **direct** = point-to-point command; **topic** = broker-side routing where
each consumer binds the keys it wants (so `users.deleted` is simply dropped); **fanout** = broadcast to
two independent subscribers. Topology (exchanges/queues) is owned by the RabbitMQ topology operator in
the deployment repo — the binaries only publish and consume, reconnecting on failure. The message
shapes and topology names live in [`internal/messages`](internal/messages).

## How it works

- **Spec-driven server.** The HTTP server interface is generated from
  [`api/openapi.yaml`](api/openapi.yaml) with
  [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) (chi server).
  Regenerate with `make generate`; never edit `api/server.gen.go` by hand.
- **Postgres.** The manager connects via `pgx` (through the `database/sql` adapter).
- **Migrations.** [`rubenv/sql-migrate`](https://github.com/rubenv/sql-migrate)
  runs the embedded SQL in [`data/migrations`](data/migrations) on startup — a single
  `users` table (`id UUID`, `created_at TIMESTAMPTZ`).

## Configuration

Postgres (manager only):

| Env var        | Purpose                                                              |
| -------------- | ------------------------------------------------------------------- |
| `PG_PASSWORD`  | Password for the in-cluster DSN (production).                       |
| `PG_HOST` / `PG_PORT` / `PG_USER` / `PG_DATABASE` | Connection parts (defaulted).    |
| `PORT`         | HTTP listen port (default `8080`).                                  |

### Messaging (all binaries) — **required**, no defaults

Every messaging variable must be set; a missing one fails startup with an error naming all that are
missing. Only connection + identity are configured per-pod — the exchange/routing-key names are
compile-time constants in `internal/messages`, so they can't be misconfigured. Consumers derive their
own queue name (`<WORKLOAD_NAME>.<exchange>`) to match the topology operator's naming.

| Env var                                   | Purpose                                                    |
| ----------------------------------------- | ---------------------------------------------------------- |
| `WORKLOAD_NAME`                           | This workload's identity (message `sender`; queue prefix). |
| `RABBITMQ_HOST` / `RABBITMQ_PORT`         | Broker address.                                            |
| `RABBITMQ_VHOST`                          | Virtual host.                                              |
| `RABBITMQ_USERNAME` / `RABBITMQ_PASSWORD` | Credentials (operator-generated Secret in-cluster).        |

## Develop

```sh
make generate    # regenerate the server from the spec
make build       # build all three binaries (bin/manager, bin/signup, bin/auditor)
make test        # run tests (spins up a Postgres container via Docker)
make run         # run the manager locally
make run-signup  # run the signup service locally
make run-auditor # run the auditor service locally
```

## Tests

`internal/handler/handler_test.go` starts a real Postgres
([testcontainers-go](https://golang.testcontainers.org/)), runs the migrations, seeds a couple of
users, and asserts `GET /users` returns them as JSON. Requires a running Docker daemon.
`internal/messages` and `internal/mq` carry broker-free unit tests.
