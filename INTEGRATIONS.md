# INTEGRATIONS.md — Upstream & Third-Party Integrations

How **AI_LM** exchanges data with the outside world. AI_LM is a standalone service: it owns
no business data of record, so virtually all of its inputs come from **one upstream — the
GableLBM ERP** — over a single authenticated contract. This file is the consumer-side view
of that contract (the provider-side view lives in GableLBM's `INTEGRATIONS.md`). Keep it in
sync with `backend/internal/gable/client.go` and the catalog resolver in
`backend/internal/catalog/service.go`.

## Integration surfaces at a glance

| Direction | Surface | Auth | Peer | Type |
|---|---|---|---|---|
| **Outbound** (read) | `GET {GABLE_API_URL}/api/integration/*` | `X-Integration-Key` header | **GableLBM** | ecosystem |
| **Outbound** (write-back) | `POST {GABLE_API_URL}/api/integration/delivery-routes` | `X-Integration-Key` header | **GableLBM** | ecosystem |
| **Inbound** (served) | `/api/v1/*` (fleet, catalog, load, routing, compliance) | JWKS JWT (`AUTH_MODE=dev` bypass) | AI_LM's own Lit UI | first-party |

AI_LM has **no true third-party integrations of its own** — no EDI, no ERP adaptors, no
co-op feeds. Those live in GableLBM. AI_LM's entire integration story is "consume one ERP's
contract"; the contract is deliberately ERP-agnostic so AI_LM stays licensable (see
[Replacing the backend](#replacing-the-backend)).

---

## 1. The GableLBM contract (the licensing surface)

Every outbound call is gated by the `X-Integration-Key` header, set to
`GABLE_INTEGRATION_KEY` (must equal GableLBM's `INTEGRATION_API_KEY`). The base URL is
`GABLE_API_URL` (e.g. `https://demo.gablelbm.com` on staging — public, not a secret). The
client is `internal/gable.Client`; config resolution is in `internal/config/config.go`.

### Consumed routes

| Method | Path | AI_LM consumer | Purpose |
|---|---|---|---|
| `GET` | `/api/integration/products` | `gable.Client.GetProductsWithWeight` | Catalog pull — weight **+ 3D geometry** |
| `GET` | `/api/integration/vehicles` | `internal/fleet` | Fleet → axle/bed profiles keyed by gable id |
| `GET` | `/api/integration/drivers` | `internal/routing` | Driver assignment for route write-back |
| `GET` | `/api/integration/orders` | `internal/catalog` / routing | Orders + line items + delivery geo |
| `POST` | `/api/integration/delivery-routes` | `internal/routing` (approve) | Write-back of an approved route plan |

> GableLBM also exposes quote endpoints (`bulk-price`, `quotes`, `accept-and-convert`) on
> the same `/api/integration/*` prefix; AI_LM does **not** consume those today.

### `GET /api/integration/products` — catalog + geometry (the crux)

Called by `GetProductsWithWeight()` with **no query params**, which triggers GableLBM's
**bulk catalog pull** (`LIMIT 1000`) — AI_LM hydrates its entire load-planning catalog in a
single call. With a `category` or `q` filter the same endpoint returns a `LIMIT 20`
typeahead list, which AI_LM does not use.

> **Why no-params matters:** prior to GableLBM commit `b5170de` the endpoint required a
> filter and returned `400` on an empty request. That guard was removed precisely so this
> bulk pull works. An empty `?q=` still counts as "no filter" but relying on it is fragile —
> AI_LM sends **no params at all**.

Each row decodes into `gable.Product` (`internal/gable/client.go`):

```json
{
  "sku": "LUM-21216-NO2",
  "description": "2x12x16 Hem-Fir No.2",
  "category": "Lumber",
  "base_price": 54.0,
  "weight_lbs": 54,
  "length_in": 192.0,
  "width_in": 11.25,
  "height_in": 1.5,
  "stackable": true,
  "geometry_source": "parametric"
}
```

`length_in` / `width_in` / `height_in` are **nullable** — decoded as `*float64` (pointers)
so "PIM has no geometry yet" (`null`) is distinct from a real `0`. `stackable` is likewise a
`*bool`. A `null`-dimension product resolves to **FALLBACK** (`has_geometry=false`) so the
Load Builder flags the SKU instead of rendering a zero-size box.

### Geometry resolution (owned by AI_LM)

AI_LM merges this payload with its own optional per-product overrides and resolves the
winning geometry **OVERRIDE → PIM → FALLBACK** in `catalog.Service.resolveGeometry` →
`ListEffectiveProducts`, surfaced at `GET /api/v1/catalog/products` as `[]EffectiveProduct`.
GableLBM stays canonical; AI_LM stays portable. The shared render contract is
**1 inch = 1/12 Three.js world unit** — identical to GableLBM's `<gable-product-twin-3d>`,
so a 96″ board is `8` world units in both apps. See `ARCHITECTURE.md` §4 and `CLAUDE.md`
"Digital-Twin Geometry Resolution".

When GableLBM is unreachable, `GET /api/v1/catalog/products` returns **`502 Bad Gateway`** so
the UI distinguishes an upstream outage from a genuinely empty catalog.

### `GET /api/integration/vehicles` / `drivers`

`vehicles` returns id, name, type, capacity, make/model/year. AI_LM's `fleet` module keys
its own **axle configuration + bed dimensions** to the GableLBM vehicle id (GableLBM models
`capacity_weight_lbs` but no axle/bed profile). `drivers` feeds route write-back.

### `GET /api/integration/orders`

Orders + line items (`product_id, sku, quantity, weight_lbs`) + delivery `latitude/longitude`
where present. Drives the daily-order routing optimizer. *(Note: order lines carry weight but
not per-line geometry today — building a load straight from an order still needs a catalog
join; see the roadmap in `CLAUDE.md`.)*

### `POST /api/integration/delivery-routes` — write-back

AI_LM posts an approved plan back:
`{ vehicle_id, driver_id, scheduled_date, stops[]{order_id, sequence, lat, lng} }`.
Idempotent on `(vehicle_id, scheduled_date)` — re-approving a plan overwrites rather than
duplicates.

---

## 2. Inbound surface AI_LM serves (`/api/v1/*`)

AI_LM's own Lit UI is the only consumer of its `/api/v1/*` API (fleet, catalog, load,
routing, compliance). Authenticated by JWKS-verified JWT via `pkg/middleware`;
`AUTH_MODE=dev` bypasses it on local/staging (share GableLBM's `JWKS_URL` for a real auth
path). Write routes additionally require
`RequireRole("admin","owner","dispatcher","yard")`. This is not a third-party surface — it
is documented here only to complete the picture. Full route table is in `CLAUDE.md`.

---

## Replacing the backend

The five consumed routes above (four reads + the delivery-routes write-back) are the **entire**
dependency AI_LM has on GableLBM. Any ERP that satisfies this contract — same paths, same
`X-Integration-Key` auth, same product/vehicle/driver/order shapes, and accepting the
route write-back — can host AI_LM **unchanged**. That is the deliberate licensing seam: the
ERP-specific knowledge is confined to `internal/gable`, and everything downstream
(`fleet`, `catalog`, `load`, `routing`, `compliance`) speaks AI_LM's own types.

## Auth quick reference

| Surface | Mechanism | Where |
|---|---|---|
| Outbound → GableLBM `/api/integration/*` | `X-Integration-Key` == GableLBM `INTEGRATION_API_KEY` | `internal/gable/client.go`, `GABLE_INTEGRATION_KEY` env |
| Inbound `/api/v1/*` | JWKS-verified JWT (`AUTH_MODE=dev` bypass on staging) | `pkg/middleware`, `JWKS_URL` env |

## Config

| Env var | Meaning | Secret? |
|---|---|---|
| `GABLE_API_URL` | GableLBM base URL (e.g. `https://demo.gablelbm.com`) | no |
| `GABLE_INTEGRATION_KEY` | `X-Integration-Key` value; must equal GableLBM's `INTEGRATION_API_KEY` | **yes** |
| `JWKS_URL` | JWKS for inbound JWT verification (omit when `AUTH_MODE=dev`) | no |
| `AUTH_MODE` | `dev` bypasses inbound auth on staging | no |

See `DEVOPS.md` for how these are set on the `ai-lm-staging` DO app.
