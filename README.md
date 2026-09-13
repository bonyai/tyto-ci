# Tyto CI

Managed GitHub Actions and GitLab CI runners backed by ephemeral Tyto
sandboxes. A developer selects an administrator-owned runner profile; they do
not receive sandbox credentials.

```yaml
# .github/workflows/test.yml
jobs:
  test:
    runs-on: tyto
    permissions:
      contents: read
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - run: npm ci
      - run: npm test
```

```yaml
# .gitlab-ci.yml
test:
  tags: [tyto]
  id_tokens:
    TYTO_ID_TOKEN:
      aud: https://api.tyto.run
  script:
    - npm ci
    - npm test
```

## Implemented phases

1. **Control-plane scheduling** — signed GitHub `workflow_job` and GitLab
   build webhooks, administrator-owned `tyto`, `tyto-large`, and `tyto-gpu`
   profiles, per-profile concurrency limits, idempotent scheduling, and
   durable job-to-sandbox state.
2. **Secure identity boundary** — RS256/JWKS OIDC verification primitives for
   GitHub Actions and GitLab job ID tokens. Tokens and provider credentials are
   never put in state files.
3. **Provider runner integration** — a GitHub REST client for one-use JIT
   runner configuration and validated GitLab custom-executor configuration.
4. **Runner compatibility policy** — v1 explicitly allows shell, JavaScript,
   and composite actions; it rejects Docker/container jobs and service
   containers before provisioning.
5. **Lifecycle operations** — crash-safe local state, restart recovery,
   automatic expired-lease reaping, `/healthz`, `/readyz`, and an aggregate
   Prometheus-compatible `/metrics` endpoint.

## Run locally

```sh
TYTO_CI_GITHUB_WEBHOOK_SECRET=... \
TYTO_CI_GITLAB_WEBHOOK_TOKEN=... \
TYTO_CI_STATE_FILE=./tyto-ci-state.json \
go run ./cmd/tyto-ci
```

The service listens on `:8080` by default. Set `TYTO_CI_LISTEN_ADDR` to
override it. State files are mode `0600` and must be stored on a persistent
volume; a multi-replica deployment needs a transactional shared `ci.Store`
implementation (for example PostgreSQL), not the local file store.

Endpoints:

- `POST /webhooks/github` — verifies `X-Hub-Signature-256` and accepts queued
  `workflow_job` events.
- `POST /webhooks/gitlab` — verifies `X-Gitlab-Token` and accepts pending
  build events.
- `GET /healthz`, `GET /readyz`, `GET /metrics` — non-sensitive operational
  status only. Detailed job state intentionally has no unauthenticated route.

## Production wiring still required

This repository intentionally has no fake sandbox provisioning. The Tyto
compute API must first support trusted CI metadata plus server-enforced leases:
the provider, provider job ID, project identity, selected profile, expiry, and
an orphan-safe delete operation. Implement `ci.Provisioner` against that API
and pass it to `ci.NewSchedulerWithStore` in place of
`ci.UnconfiguredProvisioner`.

An operator must also configure the following outside the repository:

- a GitHub App (webhook secret, installation-token minting, and Actions runner
  permission), then supply its short-lived installation token to
  `provider.GitHubJITClient` per job;
- a GitLab hosted/custom runner registration and its runtime-only runner token;
- trusted GitHub/GitLab OIDC issuer, JWKS URL, audience, and project claim
  configuration for `oidc.Verifier`;
- a CI-ready Tyto template containing the official GitHub runner or GitLab
  Runner, plus a control-plane sweeper as the final lease-enforcement backstop.

Run verification with `go test ./...`.
