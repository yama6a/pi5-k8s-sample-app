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
make ci          # everything CI runs: tidy, generate, fmt, lint, vet, test, vuln
make generate    # regenerate the server from the spec
make build       # build all three binaries (bin/manager, bin/signup, bin/auditor)
make test        # run tests (spins up a Postgres container via Docker)
make run         # run the manager locally
make run-signup  # run the signup service locally
make run-auditor # run the auditor service locally
```

Lint runs against the canonical config from [yama6a/gha](https://github.com/yama6a/gha), fetched into
`.build/` by `make lint-config`. There is no `.golangci.yaml` here.

## Dependency updates

Renovate bumps every pin in the repo: Go modules and the `go` directive (`gomod`), the base images in
`.build/Dockerfile` (`dockerfile`, digest-pinned), and the action refs in `.github/workflows` (`github-actions`,
digest-pinned).

- Config: [`renovate.json5`](renovate.json5)
- Runner: [`.github/workflows/renovate.yaml`](.github/workflows/renovate.yaml), the shared workflow from
  [yama6a/gha](https://github.com/yama6a/gha), twice a night plus `workflow_dispatch`

One-time setup:

1. Create a PAT. Fine-grained: this repo, Contents + Pull requests + Workflows + Issues read-write. Or classic:
   `repo` + `workflow`.
2. Add it as the repo secret `RENOVATE_TOKEN`. The built-in `GITHUB_TOKEN` cannot open PRs that re-trigger
   workflows and lacks the scope.
3. Run the workflow by hand. It populates the dependency-dashboard issue and opens the first PRs (one of them
   pins every action and base image to a digest).
4. Require the `go / go` and `renovate-config` checks on `main`, no required reviews. Renovate cannot
   approve its own PR, so a required review deadlocks the automerge.

```bash
gh api -X PUT repos/yama6a/pi5-k8s-sample-app/branches/main/protection \
  -H "Accept: application/vnd.github+json" --input - <<'JSON'
{
  "required_status_checks": { "strict": false, "checks": [{"context": "go / go"}, {"context": "renovate-config"}] },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null
}
JSON
```

Non-major updates land in one combined PR that Renovate merges itself once CI is green. `platformAutomerge`
is off, so the merge happens on a LATER run: the first nightly run opens, the second one 30 minutes later
merges. Majors get their own reviewed PR each, except the testcontainers modules, which stay grouped with the
core module.

One thing to know: every automerged bump is a push to `main`, so build-push cuts a release, pushes a new
image tag, and opens an auto-merging PR in `offgrid-private` and `offgrid` that bumps the three charts
pinning it. Bumps and releases are 1:1.

## Tests

`internal/handler/handler_test.go` gets a migrated Postgres database from
[pgsandbox](https://github.com/yama6a/pgsandbox), seeds a couple of users, and asserts `GET /users`
returns them as JSON. The `internal/audit` and `internal/session` tests start Redis through
[testcontainers-go](https://golang.testcontainers.org/). Both need a running Docker daemon; the
`pgsandbox-16` container stays up between runs, `docker rm -f pgsandbox-16` removes it.
`internal/messages` and `internal/mq` carry broker-free unit tests.
