# Architecture — AI_LM

AI_LM (AI Load Management & Compliance) is a **standalone microservice** for GableLBM that
balances truck loads across axles (avoiding GVW/axle fines) and pre-optimizes daily
delivery routing. It is deliberately decoupled from the ERP so it can later be licensed to
other ERPs. This document covers the system shape; `CLAUDE.md` is the working guide,
`INTEGRATIONS.md` the consumer contract, `DEVOPS.md` the deploy runbook.

## 1. Principles

- **Standalone, not embedded.** Own Go binary, own PostgreSQL DB, own Lit UI. No shared
  process or schema with GableLBM — it reads/writes only over `/api/integration/*`.
- **Portable by construction.** Supplementary attributes GableLBM doesn't model
  (axle/bed profiles, per-product overrides) live in AI_LM, keyed by GableLBM UUIDs. Any
  ERP that satisfies the integration contract can host AI_LM unchanged.
- **Heuristics behind interfaces.** The load solver and route optimizer are deterministic
  heuristics implementing `load.Solver` / routing interfaces, so an AI/optimizer can
  replace them without touching callers.
- **Backend-owned merges.** Cross-source resolution (PIM geometry vs. local overrides) is
  computed in the catalog service, not the UI, so every client sees one consistent answer.

## 2. Topology

```
GableLBM (source of truth)                AI_LM (this service)
  GET  /api/integration/products  ─────▶  internal/gable     X-Integration-Key client
  GET  /api/integration/vehicles  ─────▶  internal/fleet      axle/bed profiles by gable id
  GET  /api/integration/drivers   ─────▶  internal/routing    driver assignment
  GET  /api/integration/orders    ─────▶  internal/catalog    product dims; weight from LBM
  POST /api/integration/             ◀──  internal/load       3D placement + axle/GVW solver
       delivery-routes (write-back)       internal/compliance GVW rules + restricted points
```

- **Pattern:** modular monolith — one Go binary, modules under `backend/internal/`.
- **Module shape:** `model.go`, `repository.go` (pgx), `service.go`, `handler.go`; wired in
  `backend/cmd/server/main.go`.
- **Ports:** API on **8090**; Postgres on **5434** (docker-compose mapping).
- **API:** REST JSON at `/api/v1/*`; public `/health`, `/healthz/{live,ready}`, `/metrics`.

## 3. Modules

| Module | Responsibility |
|---|---|
| `gable` | Integration client to GableLBM (`X-Integration-Key`); wire types for products (incl. geometry), vehicles, drivers, orders, route write-back. |
| `fleet` | Vehicle profiles — axle configuration and bed dimensions keyed by GableLBM vehicle id. |
| `catalog` | Per-product override store **and** the PIM-geometry resolver (`EffectiveProduct`). |
| `load` | 3D placement solver + per-axle / GVW computation (`load.Solver`). |
| `routing` | Daily-order optimizer — nearest-neighbor + 2-opt, multi-load CVRP split by capacity. |
| `compliance` | GVW rule enforcement + restricted-point (bridge/overpass) flagging via haversine buffer. |

## 4. The Digital-Twin Pipeline

The crux feature. GableLBM's PIM is canonical for per-product L/W/H; AI_LM renders each
product as a scaled box against the truck bed.

```
PIM dims (GableLBM)            AI_LM overrides            EffectiveProduct
  length/width/height   ┐      product_dimensions   ┐      geometry_source: OVERRIDE|PIM|FALLBACK
  stackable, weight     ├────▶ (non-zero = override)├────▶ has_geometry: bool
  geometry_source       ┘      default_source       ┘      → GET /api/v1/catalog/products
                                                            → YardLoadView → Load3DVisualizer
```

- **Resolution priority:** OVERRIDE → PIM → FALLBACK (`catalog.Service.resolveGeometry`).
  FALLBACK sets `has_geometry=false` so the UI flags the SKU instead of rendering a
  zero-size box.
- **Dependency injection:** `*gable.Client` satisfies the `productSource` interface and is
  injected into `catalog.Service` (so the resolver is unit-testable with a fake source and
  degrades to overrides-only when nil).
- **Render contract:** `Load3DVisualizer.ts` `_scale = 1/12` — **1 inch = 1/12 Three.js
  world unit**, identical to GableLBM's `<gable-product-twin-3d>`. Solver coordinates are
  inches from the front-left-floor corner; the frontend maps solver `(x,y,z)` → three
  `(x,z,y)` (Y-up) and multiplies by `_scale`.
- **Failure mode:** `GET /api/v1/catalog/products` returns `502` when GableLBM is
  unreachable, distinguishing an outage from an empty catalog.

## 5. Frontend

- **Lit 3** Light-DOM web components, all `ailm-` prefixed; Tailwind "Industrial Dark".
- **3D:** `<ailm-load-3d-visualizer>` (Three.js). **Maps:** `<ailm-route-map>` (Leaflet).
  **Charts:** Chart.js.
- **Load Builder** (`YardLoadView.ts`): vehicle select → add real products from the
  resolved catalog → Optimize → axle bars + GVW pass/warn/fail + scaled 3D twins. Shows an
  Amber-Warn banner counting catalog products with `has_geometry=false`.
- **HTTP:** `services/aiLmService.ts` (typed) over `services/fetchClient.ts`; pages never
  call `fetch` directly. Relative `/api/v1/*` — Vite proxies to `:8090` in dev, nginx in
  prod.

## 6. Data & Auth

- PostgreSQL 16 via pgx v5. UUID PKs; `DECIMAL(19,4)` quantities; money as BIGINT cents in
  native tables; weights/dims as integers where exact; lat/lng `DOUBLE PRECISION`.
- Migrations in `backend/migrations/` (forward `NNN_*.sql` + sibling `_down.sql` the
  migrator skips). Current core: `001_ai_lm_core`.
- JWT via JWKS (`pkg/middleware`); `AUTH_MODE=dev` bypasses for local/demo (share GableLBM's
  JWKS in prod). Integration calls outbound carry `X-Integration-Key`.

## 7. Deployment

Digital Ocean App Platform, auto-deploy on push. Live env is **`ai-lm-staging`** (tracks
`main`), pointed at GableLBM `community` (`https://demo.gablelbm.com`). Full topology, app
IDs, and the `doctl` runbook are in `DEVOPS.md`.
