# AI_LM — AI Load Management & Compliance Module

A standalone microservice for [GableLBM](../GableLBM-main) that helps independent lumber &
building-materials (LBM) dealers avoid axle/GVW fines and replace subjective manual
dispatch with optimized, compliance-aware routing.

## Two pillars

1. **Load Optimization** — a 3D visual model of a truck/trailer load (Three.js) that
   auto-balances material across axles and gives yard staff clear loading guidance, with a
   live GVW pass/warn/fail check.
2. **Compliance & Routing** — daily-order routing that returns a pre-optimized
   (80–90% complete) draft for the dispatcher, enforces gross-vehicle-weight limits during
   load-building, and flags restricted points (weight/height/width-limited bridges &
   overpasses) along the route.

AI_LM is **standalone**: its own Go backend, its own PostgreSQL database, and its own Lit
UI. It reads the source of truth (orders, products + weights, vehicles, delivery geo) from
GableLBM over an integration API and writes approved routes back the same way — so the
ERP schema stays untouched and the module stays portable for commercial licensing.

## Stack

- **Backend:** Go 1.25, stdlib `net/http.ServeMux`, pgx v5, PostgreSQL 16, Prometheus.
- **Frontend:** Lit 3 + TypeScript + Vite + Tailwind, Three.js (3D), Leaflet (maps),
  Chart.js. Industrial Dark design system, `ailm-` Light-DOM components.

## Quick start (local)

```bash
# 1. Postgres on :5434 (db ai_lm_db)
docker compose up -d

# 2. Backend (:8090)
cd backend
cp .env.example .env            # set GABLE_API_URL / GABLE_INTEGRATION_KEY
go run ./cmd/migrate            # apply schema
go run ./cmd/seed               # demo fleet profiles + restricted points
go run ./cmd/server             # serve API; hit /healthz/live and /metrics

# 3. Frontend (:5173, proxies /api → :8090)
cd ../app
npm install
npm run dev
```

Point `GABLE_API_URL` at a running GableLBM instance (default `http://localhost:8080`) and
set `GABLE_INTEGRATION_KEY` to its `INTEGRATION_API_KEY` to pull live orders/vehicles and
write approved routes back.

## Layout

```
backend/   Go service — internal/{gable,fleet,catalog,load,routing,compliance}
app/       Lit app — pages/{DispatchBoard,YardLoadView,FleetProfiles,CompliancePoints}
.do/       Digital Ocean App Platform manifests
```

See [CLAUDE.md](./CLAUDE.md) for architecture, conventions, the API surface, and the
GableLBM integration contract.

## License

Community-owned open-source core for GableLBM.
