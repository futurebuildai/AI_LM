# DEVOPS.md — Deployment Source of Truth

Operational source-of-truth for **AI_LM** deployments. Pairs with the specs in `.do/` and
with `INTEGRATIONS.md` (the upstream GableLBM contract). When the deploy topology changes,
update **this file first**.

## Platform

Digital Ocean App Platform (PaaS, Dockerfile-based, auto-deploy on push). Inspected and
managed via **`doctl`** (`~/.local/bin/doctl`, authenticated, default context) — not the
web console.

## What is actually deployed

| Spec | Branch | DO App | App ID | URL | Logical DB |
|---|---|---|---|---|---|
| `.do/app-staging.yaml` | `main` | **`ai-lm-staging`** | `8a274c57-dee2-4053-ac3c-40fe2528ca9e` | **https://load.gablelbm.com** (+ https://ai-lm-staging-b6ssv.ondigitalocean.app) | `ai_lm_staging` |
| `.do/app-demo.yaml` | `community` | *(not created)* | — | (intended `demo.ai-lm.gable.com`) | `ai_lm_demo` |

> **Custom domain:** `load.gablelbm.com` is declared in `app-staging.yaml`
> (`domains:` block). gablelbm.com's DNS zone is **not** in this DO account, so the
> DNS provider needs `CNAME load.gablelbm.com → ai-lm-staging-b6ssv.ondigitalocean.app`;
> App Platform provisions the TLS cert once the record resolves. Check status with
> `doctl apps get 8a274c57-dee2-4053-ac3c-40fe2528ca9e --output json | jq '.[0].domains'`.
>
> **Functional dependency:** the guided workflow needs GableLBM `community` to carry
> the AI_LM dispatch support (migration 075, scheduled-date orders, demo seed,
> manifest storage, yard Pack Trucks) — see
> https://github.com/futurebuildai/GableLBM-main/pull/16. Until that merges, ingest
> matches orders by creation date only, the demo-seed button 404s, and pushed routes
> carry no packing manifest.
>
> **Known issue (2026-06-12):** ingest against demo.gablelbm.com returns
> `401 invalid integration key` — `GABLE_INTEGRATION_KEY` (ai-lm-staging) does not
> match `INTEGRATION_API_KEY` (gablelbm-demo). Both are encrypted DO secrets, so the
> fix needs someone holding the value: either paste gablelbm-demo's key into
> ai-lm-staging, or rotate BOTH to a fresh shared value (coordinate with FB Brain,
> which uses the same GableLBM key).

> **Important reality check:** `ai-lm-staging` (tracking **`main`**) is the **only live
> AI_LM environment**. `app-demo.yaml` targets a `community` branch and a `demo.ai-lm.gable.com`
> domain, but **AI_LM has no `community` branch and no `ai-lm-demo` app exists in DO**. Treat
> `app-demo.yaml` as a not-yet-provisioned template. Verify with
> `doctl apps list --format ID,Spec.Name,DefaultIngress` — only `ai-lm-staging` appears.

## Integration target

`ai-lm-staging` is wired to GableLBM's **`community`** demo:
`GABLE_API_URL=https://demo.gablelbm.com` (public, in the spec) and `GABLE_INTEGRATION_KEY`
(encrypted secret, must equal GableLBM's `INTEGRATION_API_KEY`). The Load Builder's catalog
is hydrated from that GableLBM via the unfiltered bulk product pull — see `INTEGRATIONS.md`.

## Deploy anatomy

```
git push origin main ──▶ DO App Platform pulls main
                         ├─ build backend/Dockerfile → main + migrate + seed (port 8090)
                         ├─ build app/Dockerfile      → nginx + Vite SPA (port 8080)
                         ├─ deploy backend + frontend
                         └─ POST_DEPLOY job: sh -c "./migrate && ./seed"
```

- App Platform path-routes `/api`, `/healthz`, `/metrics` to the backend (with
  `preserve_path_prefix: true` — without it the Go router 404s on `/healthz/live`); `/` to
  the frontend. `CORS_ORIGINS` is intentionally unset on staging (same-origin path routing).
- `INSTANCE_COUNT` must stay **1** (in-memory middleware/state).
- The post-deploy job sets `AUTH_MODE=dev` on itself so migrate/seed don't trip the
  fail-closed `JWKS_URL` requirement (the job serves no auth'd HTTP).
- Healthy deploy = Phase `ACTIVE`, all steps green (e.g. `13/13`).

## Runbook (`doctl`)

```bash
# Confirm only ai-lm-staging is live
doctl apps list --format ID,Spec.Name,DefaultIngress

# Watch the newest deployment
doctl apps list-deployments 8a274c57-dee2-4053-ac3c-40fe2528ca9e \
  --format ID,Cause,Phase,Progress,Created | head

# Logs: service, build, and the post-deploy migrate/seed job
doctl apps logs 8a274c57-dee2-4053-ac3c-40fe2528ca9e backend          --type run
doctl apps logs 8a274c57-dee2-4053-ac3c-40fe2528ca9e backend          --type build
doctl apps logs 8a274c57-dee2-4053-ac3c-40fe2528ca9e migrate-and-seed --type run

# Force a redeploy (e.g. re-run seed); push a changed spec
doctl apps create-deployment 8a274c57-dee2-4053-ac3c-40fe2528ca9e --force-rebuild
doctl apps update            8a274c57-dee2-4053-ac3c-40fe2528ca9e --spec .do/app-staging.yaml
```

### Deploy + verify

```bash
git push origin main
doctl apps list-deployments 8a274c57-dee2-4053-ac3c-40fe2528ca9e \
  --format Cause,Phase,Progress,Created | head -3      # wait for ACTIVE

BASE=https://ai-lm-staging-b6ssv.ondigitalocean.app
curl -6 --retry 4 --retry-all-errors -sf "$BASE/healthz/live" && echo OK
curl -s "$BASE/api/v1/catalog/products" | jq 'length, (map(.geometry_source) | group_by(.) | map({(.[0]): length}))'
# expect >0 products; a few PIM/has_geometry=true, the rest FALLBACK
```

The host's IPv6 path can be flaky — `curl -6 --retry 4 --retry-all-errors` is the reliable
incantation.

## Secrets

`DATABASE_URL` (DO binding `${ai-lm-pg-staging.DATABASE_URL}`) and `GABLE_INTEGRATION_KEY`
are the only secrets, both encrypted env vars; never inline them in YAML. `GABLE_API_URL`
is a public base URL (not secret). `AUTH_MODE=dev` ⇒ no JWKS/JWT key needed on staging; for
a real auth path set `JWKS_URL` (share GableLBM's) and unset `AUTH_MODE`.

## Rollback

```bash
doctl apps list-deployments 8a274c57-dee2-4053-ac3c-40fe2528ca9e
doctl apps create-deployment 8a274c57-dee2-4053-ac3c-40fe2528ca9e --restore-deployment <deployment-id>
```

The migrate/seed job re-runs (idempotent). DO rollback does not undo schema changes — fix a
bad migration with a corrective forward migration.

## Note: deploys are not GitHub Actions

Deployment is DO `deploy_on_push`, not CI. `gh run list` won't show deploy status; use
`doctl apps list-deployments`.
