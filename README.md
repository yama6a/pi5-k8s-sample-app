# pi5-k8s-sample-app

A Go demo service for the [offgrid](https://github.com/yama6a/offgrid) platform. It uses Postgres, two Redis
instances and all three RabbitMQ exchange types. offgrid runs one image as three workloads.

## Three binaries, one image

| Binary | Workload | Does |
| --- | --- | --- |
| `/manager`, the default entrypoint | sample-user-manager | Serves `GET /users` from Postgres and `GET /audit` from Redis. Per `create-user-command` it stores the user, emits `users.created` and an audit message, and opens a session. Above `maxUsers` it deletes the oldest user and emits `users.deleted` and an audit message |
| `/signup` | sample-user-signup | Publishes a `create-user-command` on a fixed interval. Logs `users.created` events and every audit message |
| `/auditor` | sample-audit-logger | Logs every audit message |

## Messaging topology

| Exchange | Type | Published by | Consumed by |
| --- | --- | --- | --- |
| `create-user-command` | direct: one command queue | signup | manager |
| `user-events` | topic: each queue binds the keys it wants | manager: `users.created`, `users.deleted` | signup, which binds only `users.created` |
| `user-audit-logger` | fanout: every bound queue gets every message | manager | signup and auditor |

- No queue binds `users.deleted`, so the broker drops it. That shows topic routing.
- The Messaging Topology Operator in offgrid declares every exchange and queue. The binaries only publish and
  consume, and reconnect after any failure. So a pod that starts before its queue exists recovers by itself.
- The names are constants in [`internal/messages`](internal/messages), so no pod can misconfigure them. They
  must match the topology values in offgrid's sample charts.
- Consumers use autoAck. A message whose handler fails is lost. The demo shows routing, not delivery guarantees.

## Design

- **Spec-driven server.** [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) generates the chi
  server interface from [`api/openapi.yaml`](api/openapi.yaml). Run `make generate` after a spec change. Never
  edit `api/server.gen.go` by hand.
- **Postgres** through `pgx` and the `database/sql` adapter.
  [`rubenv/sql-migrate`](https://github.com/rubenv/sql-migrate) runs [`data/migrations`](data/migrations) at
  startup.
- **Two Redis instances.** The audit instance is a cache: a user's list expires an hour after its latest event.
  The sessions instance is persistent, so sessions survive a restart.
- **No Redis password.** A CiliumNetworkPolicy in offgrid limits each instance to the manager.

## Configuration

Manager only, all optional:

| Env var | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `PG_HOST`, `PG_PORT`, `PG_USER`, `PG_DATABASE` | the in-cluster CNPG Service, `5432`, `app`, `app` | Postgres connection |
| `PG_PASSWORD` | empty | Postgres password |
| `REDIS_ADDR`, `REDIS_PASSWORD` | the in-cluster cache Service, empty | audit Redis |
| `REDIS_SESSIONS_ADDR`, `REDIS_SESSIONS_PASSWORD` | the in-cluster sessions Service, empty | sessions Redis |

All binaries, required. A missing one fails startup, and the error names every missing variable:

| Env var | Purpose |
| --- | --- |
| `WORKLOAD_NAME` | the `service` field of audit messages, and the queue name prefix |
| `RABBITMQ_HOST`, `RABBITMQ_PORT` | broker address |
| `RABBITMQ_VHOST` | virtual host |
| `RABBITMQ_USERNAME`, `RABBITMQ_PASSWORD` | credentials, from the Secret the topology operator generates |

A consumer derives its queue name as `<WORKLOAD_NAME>.<exchange>`, the name the topology chart generates.

## Develop

```sh
make ci          # everything CI runs: tidy, generate, fmt, lint, vet, test, vuln
make generate    # regenerate the server from the spec
make build       # build bin/manager, bin/signup and bin/auditor
make test        # run the tests. Needs a running Docker daemon
make run         # run the manager locally
make run-signup  # run the signup service locally
make run-auditor # run the auditor service locally
```

`make lint-config` fetches the shared golangci config from [yama6a/gha](https://github.com/yama6a/gha) into
`.build/`. A `.golangci.local.yaml` in the repo root, if present, is merged on top.

## Tests

- `internal/handler` clones a migrated Postgres from [pgsandbox](https://github.com/yama6a/pgsandbox). The
  `pgsandbox-<major>` container stays up between runs. Remove it with `docker rm -f pgsandbox-<major>`.
- `internal/audit` and `internal/session` start Redis through
  [testcontainers-go](https://golang.testcontainers.org/).
- `internal/messages` and `internal/mq` need no broker.

## Dependency updates

Renovate bumps the Go modules, the `go` directive, the base images in `.build/Dockerfile` and the action pins.
Rules: [`renovate.json5`](renovate.json5). Runner: [`.github/workflows/renovate.yaml`](.github/workflows/renovate.yaml),
daily. Setup: [Renovate runbook](docs/runbooks/renovate.md).

- Non-major bumps land in one combined PR, which GitHub merges once CI is green.
- Each major gets its own PR. A Copilot backward-compatibility check turns on auto-merge when it rates the
  major safe. The testcontainers modules share one major PR, because they must match the core module.
- Every merge to `main` cuts a release, pushes an image tag, and opens an auto-merging PR in offgrid and
  offgrid-private that bumps the three charts. So every automerged bump ships.
