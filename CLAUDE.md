# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What Is This?

**AI_LM** (AI Load Management & Compliance Module) is a standalone microservice for
independent lumber & building-materials (LBM) dealers running on the **GableLBM** ERP.
It tackles two costly problems: aggressive truck weight enforcement (axle/GVW fines) and
subjective, manual dispatch. It does so with two pillars:

1. **Load Optimization** — a 3D visual model (Three.js) of a selected truck/trailer's
   load that auto-calculates material placement to balance weight across axles and gives
   yard staff clear loading guidance.
2. **Regulatory Compliance & Routing** — daily-order routing that returns a pre-optimized
   (80–90% complete) route + load config for dispatcher fine-tuning, with **GVW
   enforcement** during load-building and **restricted-point flagging** (weight/height/
   width-limited bridges & overpasses) along the route.

**Strategic intent:** a community-owned open-source core for GableLBM, architected as a
**standalone service** so it can later be commercially licensed to third-party ERPs via a
clean integration API.

## Relationship to GableLBM

AI_LM is a **standalone service + integration API** — its own backend, own Postgres DB,
own Lit UI. It does **not** share a process or schema with GableLBM. It pulls the source
of truth (orders, products+weight, vehicles, delivery geo) from GableLBM via
`GET /api/integration/*` using an `X-Integration-Key`, and writes approved routes back via
`POST /api/integration/delivery-routes`.

**Why AI_LM keeps its own fleet/catalog dimension data:** GableLBM products carry
`weight_lbs` but no L/W/H; vehicles carry `capacity_weight_lbs` but no axle config or bed
dimensions. Rather than mutate the ERP schema (which would hurt portability to other
ERPs), AI_LM stores these supplementary attributes itself, keyed by GableLBM UUIDs, with
sensible UOM-based defaults. This is what makes the module portable for commercial
licensing.

The Go module path is `github.com/futurebuildai/ai-lm`.

## Repo Structure

```
app/          → Lit 3 frontend (Vite + TypeScript + Tailwind + Three.js + Leaflet)
backend/      → Go backend (stdlib http.ServeMux + pgx + PostgreSQL)
.do/          → Digital Ocean App Platform manifests
docker-compose.yml  → Local Postgres (:5434, db ai_lm_db)
```

## Tech Stack

Mirrors GableLBM/GableRun exactly for cross-repo consistency.

### Backend
- **Language:** Go 1.25 (`backend/go.mod`, module `github.com/futurebuildai/ai-lm`)
- **Router:** Go 1.22+ stdlib `net/http.ServeMux`. Modules expose
  `RegisterRoutes(mux, guard ...func(http.Handler) http.Handler)`.
- **Database:** PostgreSQL 16+ via pgx v5 (`pkg/database` wraps `*pgxpool.Pool`)
- **Auth:** JWT verified against JWKS (`pkg/middleware.NewAuthMiddleware`). `AUTH_MODE=dev`
  disables auth for local dev; otherwise `JWKS_URL` is required (fail-closed). Share the
  GableLBM JWKS in production.
- **Metrics:** Prometheus at `/metrics`; DB pool collector.
- **Port:** **8090** (avoids GableLBM :8080 and Vite :5173).

### Frontend
- **Framework:** Lit 3 Web Components + TypeScript 5.9 + Vite 7
- **Styling:** Tailwind CSS 3.4 + Industrial Dark tokens
- **Components:** Custom **`ailm-`** prefixed web components, **Light DOM**
  (`createRenderRoot() { return this; }`) so Tailwind utilities apply
- **Routing:** Custom SPA router in `app/src/lib/router.ts`. Route table is
  `app/src/routes.ts`.
- **3D:** Three.js (`<ailm-load-3d-visualizer>`) | **Maps:** Leaflet
  (`<ailm-route-map>`, dark CARTO tiles) | **Charts:** Chart.js | **Icons:** Lucide via
  `lib/icons.ts`
- **HTTP:** `services/fetchClient.ts` (`fetchWithAuth`) + `services/aiLmService.ts`
  (typed client). Never call `fetch` directly from pages. The app uses relative
  `/api/v1/*` paths; Vite proxies them to `:8090` in dev, nginx reverse-proxies in prod.

## Architecture

```
GableLBM (source of truth)            AI_LM
  /api/integration/products   ─────▶  internal/gable  (X-Integration-Key client)
  /api/integration/vehicles   ─────▶  internal/fleet  (axle/bed profiles by gable id)
  /api/integration/orders     ─────▶  internal/catalog(product dims; weight from LBM)
  /api/integration/             ◀───  internal/load   (3D placement + axle/GVW solver)
       delivery-routes (write-back)   internal/routing(daily-order optimizer)
                                      internal/compliance (GVW rules + restricted points)
```

- **Pattern:** Modular monolith — single Go binary, modules under `backend/internal/`.
- **Module shape:** `model.go`, `repository.go` (pgx), `service.go`, `handler.go`. Wired
  in `backend/cmd/server/main.go`.
- **Solver/optimizer are deterministic heuristics behind interfaces** (`load.Solver`,
  nearest-neighbor + 2-opt routing, haversine restricted-point buffering) so an
  AI/optimizer can replace them later without touching callers.
- **API surface:** REST JSON at `/api/v1/*`; public `/health`, `/healthz/live`,
  `/healthz/ready`, `/metrics`.

### API endpoints (`/api/v1/*`)

| Method | Path | Module |
|--------|------|--------|
| GET    | `/fleet/profiles`                       | fleet |
| GET/PUT| `/fleet/profiles/{vehicleId}`           | fleet |
| GET    | `/catalog/dimensions`                   | catalog |
| GET/PUT| `/catalog/dimensions/{productId}`       | catalog |
| POST   | `/load/optimize`                        | load |
| GET    | `/load/{id}`                            | load |
| POST   | `/routing/plan`                         | routing |
| GET    | `/routing/plan/{id}`                    | routing |
| POST   | `/routing/plan/{id}/approve`            | routing (write-back) |
| POST   | `/compliance/check-route`               | compliance |
| GET/POST | `/compliance/restricted-points`       | compliance |
| PUT    | `/compliance/restricted-points/{id}`    | compliance |

Write routes are gated by `middleware.RequireRole("admin","owner","dispatcher","yard")`.

## Key Conventions

(Carried over from GableLBM.)

### Database
- PKs are UUID v4 (`uuid_generate_v4()` via `uuid-ossp`).
- Physical quantities: `DECIMAL(19,4)`. Money in AI_LM-native tables: **BIGINT cents**.
- Weights/dimensions: `BIGINT` lb and integer inches where exact; lat/lng `DOUBLE PRECISION`.
- Migrations in `backend/migrations/` as numbered SQL with a sibling `_NNN_*_down.sql`
  rollback. The migrator (`cmd/migrate`) skips `*_down.sql`. Current set: `001_ai_lm_core`.

### Backend Code
- Config: env vars with `godotenv` fallback (`internal/config/config.go`). Default DB URL
  points to **:5434** (docker-compose mapping). Integration: `GABLE_API_URL`,
  `GABLE_INTEGRATION_KEY`.
- Server entry point: `backend/cmd/server/main.go` — wires every module repo→service→handler.
- Error envelope (`pkg/httputil`): `{ "error": { "code", "message" }, "meta": { "request_id" } }`.

### Frontend Code
- Layout shell: `<ailm-app-shell>` (collapsible sidebar). Pages under `app/src/pages/`.
- All custom elements use the `ailm-` prefix.
- Design tokens in `tailwind.config.js`; never hardcode colors. Use JetBrains Mono for all
  numbers/weights/dimensions.
- Adding a page: create the component under `app/src/pages/…`, register it in
  `app/src/routes.ts`, and map its tag in `app/src/app.ts` `_pathToTag`.

### Design System

| Token | Hex | Usage |
|-------|-----|-------|
| Gable Green | `#00FFA3` | Primary actions, success, active glow |
| Deep Space | `#0A0B10` | Global background |
| Slate Steel | `#161821` | Cards, sidebar, modals |
| Safety Red | `#F43F5E` | Errors, GVW fail |
| Amber Warn | `#FBBF24` | GVW warn, near-limit axles |
| Blueprint Blue | `#38BDF8` | Technical data, links |

**Body font:** Outfit | **Data font:** JetBrains Mono | **Theme:** Industrial Dark.

## Common Commands

### Backend (`cd backend`)
```bash
go run ./cmd/server                # run API (port 8090, needs DB on :5434)
go run ./cmd/migrate               # apply SQL migrations
go run ./cmd/seed                  # seed demo fleet profiles + restricted points
go build ./... && go vet ./... && go test ./...
make run | migrate | seed | build | vet | test | tidy
```

Override DB connection when Postgres is on the standard port:
```bash
DATABASE_URL="postgres://gable_user:gable_password@localhost:5432/ai_lm_db?sslmode=disable" go run ./cmd/server
```

### Frontend (`cd app`)
```bash
npm install
npm run dev          # Vite dev server on :5173 (proxies /api → :8090)
npm run build        # tsc -b && vite build
npx tsc --noEmit     # type-check only
```

### Infrastructure (root)
```bash
docker compose up -d # Postgres on :5434 (db ai_lm_db)
```

## Integration Contract (the licensing surface)

AI_LM consumes these GableLBM endpoints (all `X-Integration-Key` gated):

- `GET  /api/integration/products?category=|q=`  → adds `weight_lbs` per unit.
- `GET  /api/integration/vehicles`               → fleet (id, name, type, capacity, make/model/year).
- `GET  /api/integration/orders?date=&status=CONFIRMED` → orders + line items
  (`product_id, sku, quantity, weight_lbs`) and delivery `latitude/longitude` where present.
- `POST /api/integration/delivery-routes`        → write-back of an approved plan
  (`vehicle_id, driver_id, scheduled_date, stops[]{order_id, sequence, lat, lng}`).
  Idempotent on `(vehicle_id, scheduled_date)`.

A different ERP can satisfy this contract to reuse AI_LM unchanged.

## Pre-Flight Checks (before declaring work done)
- `cd app && npx tsc --noEmit` (or `npm run build`)
- `cd backend && go build ./... && go vet ./... && go test ./...`
- New DB columns: UUID PKs, `DECIMAL(19,4)` for quantities, money-as-cents.
- UI uses design-system tokens (no hardcoded colors), JetBrains Mono for numbers.
- New endpoints under `/api/v1` and wired into a `RegisterRoutes` call in
  `backend/cmd/server/main.go`.

## Out of Scope (future phases)
- Real distance-matrix/Maps provider (MVP routing is a heuristic behind a pluggable interface).
- ML-based placement (MVP solver is deterministic behind `load.Solver`).
- PostGIS geometry (MVP uses lat/lng + haversine buffer).
- Commercial multi-ERP adapters + licensing/metering (API shape is designed for it).
- Horizontal scaling (single instance; in-memory middleware).
