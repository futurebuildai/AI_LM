/**
 * Typed client for the AI_LM backend (/api/v1/*). Mirrors the Go JSON shapes
 * in internal/{fleet,catalog,load,routing,compliance}. All HTTP goes through
 * fetchWithAuth so auth/timeout/retry behave consistently.
 */
import { fetchWithAuth } from './fetchClient.ts';

const BASE = '/api/v1';

// ---- shared error envelope (pkg/httputil) ------------------------------
interface ErrorEnvelope {
  error?: { code: string; message: string };
  meta?: { request_id: string };
}

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as ErrorEnvelope;
      if (body.error?.message) msg = body.error.message;
    } catch {
      /* non-JSON body */
    }
    throw new Error(msg);
  }
  return (await res.json()) as T;
}

// ---- fleet -------------------------------------------------------------
export interface Axle {
  id?: string;
  axle_number: number;
  max_weight_lbs: number;
  position_from_front_in: number;
  axle_type: string; // STEER/DRIVE/TRAILER/TAG
}

export interface VehicleProfile {
  id: string;
  gable_vehicle_id: string;
  name: string;
  bed_length_in: number;
  bed_width_in: number;
  bed_height_in: number;
  gvwr_lbs: number;
  tare_weight_lbs: number;
  axles: Axle[];
  created_at: string;
  updated_at: string;
}

export interface ProfileInput {
  name: string;
  bed_length_in: number;
  bed_width_in: number;
  bed_height_in: number;
  gvwr_lbs: number;
  tare_weight_lbs: number;
  axles: Omit<Axle, 'id'>[];
}

// ---- catalog -----------------------------------------------------------
export interface ProductDimension {
  id: string;
  gable_product_id: string;
  sku: string;
  length_in: number;
  width_in: number;
  height_in: number;
  stackable: boolean;
  weight_lbs_override?: number;
  default_source: string;
  created_at: string;
  updated_at: string;
}

// EffectiveProduct is the resolved load-planning view of a product: GableLBM
// PIM geometry merged with AI_LM overrides. geometry_source records the winning
// provider (OVERRIDE/PIM/FALLBACK) and has_geometry is false when no usable
// L/W/H exists so the Load Builder can flag the item.
export interface EffectiveProduct {
  gable_product_id: string;
  sku: string;
  name: string;
  category?: string;
  length_in: number;
  width_in: number;
  height_in: number;
  stackable: boolean;
  weight_lbs: number;
  geometry_source: 'OVERRIDE' | 'PIM' | 'FALLBACK';
  has_geometry: boolean;
}

// ---- load --------------------------------------------------------------
export interface LoadItem {
  product_id: string;
  sku: string;
  quantity: number;
  length_in: number;
  width_in: number;
  height_in: number;
  weight_lbs: number; // per-unit
  stackable: boolean;
}

export interface Placement {
  item_id: string;
  sku: string;
  x: number;
  y: number;
  z: number;
  length_in: number;
  width_in: number;
  height_in: number;
  weight_lbs: number;
  axle_group: number;
}

export interface AxleLoad {
  axle_number: number;
  weight_lbs: number;
  max_weight_lbs: number;
  utilization: number;
  status: 'PASS' | 'WARN' | 'FAIL';
}

export interface LoadPlan {
  id: string;
  gable_route_id?: string;
  gable_delivery_id?: string;
  gable_vehicle_id: string;
  placements: Placement[];
  total_weight_lbs: number;
  axle_loads: AxleLoad[];
  balance_score: number;
  gvw_status: 'PASS' | 'WARN' | 'FAIL';
  unplaced: string[];
  created_at: string;
}

export interface OptimizeRequest {
  vehicle_id: string;
  route_id?: string;
  delivery_id?: string;
  items: LoadItem[];
}

// ---- routing -----------------------------------------------------------
export interface RouteStop {
  order_id: string;
  sequence: number;
  lat: number;
  lng: number;
  address?: string;
  weight_lbs: number;
}

export interface Load {
  vehicle_id: string;
  vehicle_name: string;
  driver_id: string;
  driver_name: string;
  capacity_weight_lbs: number;
  stops: RouteStop[];
  total_weight_lbs: number;
  total_distance_mi: number;
  total_duration_min: number;
}

export interface RoutePlan {
  id: string;
  plan_date: string;
  gable_branch_id?: string;
  gable_vehicle_id?: string;
  loads: Load[];
  unassigned_stops: RouteStop[];
  stops: RouteStop[];
  total_distance_mi: number;
  total_duration_min: number;
  status: 'DRAFT' | 'APPROVED';
  created_at: string;
  updated_at: string;
}

export interface PlanRequest {
  date: string;
  branch_id?: string;
  vehicle_id?: string;
  depot_lat?: number;
  depot_lng?: number;
}

// ---- compliance --------------------------------------------------------
export interface RestrictedPoint {
  id: string;
  name: string;
  lat: number;
  lng: number;
  restriction_type: 'WEIGHT' | 'HEIGHT' | 'WIDTH' | 'SEASONAL';
  max_gross_weight_lbs?: number;
  max_axle_weight_lbs?: number;
  max_height_in?: number;
  notes: string;
  created_at: string;
  updated_at: string;
}

export type RestrictedPointInput = Omit<RestrictedPoint, 'id' | 'created_at' | 'updated_at'>;

export interface RouteCheckRequest {
  route: { lat: number; lng: number }[];
  load: { gross_weight_lbs: number; max_axle_lbs: number; height_in: number };
  buffer_miles?: number;
}

export interface ComplianceFlag {
  point: RestrictedPoint;
  distance_mi: number;
  violation: string;
  severity: 'WARN' | 'FAIL';
}

export interface RouteCheckResult {
  status: 'PASS' | 'WARN' | 'FAIL';
  flags: ComplianceFlag[];
}

// ---- service singleton -------------------------------------------------
class AiLmService {
  // fleet
  listProfiles(): Promise<VehicleProfile[]> {
    return fetchWithAuth(`${BASE}/fleet/profiles`).then((r) => jsonOrThrow(r));
  }
  getProfile(vehicleId: string): Promise<VehicleProfile> {
    return fetchWithAuth(`${BASE}/fleet/profiles/${vehicleId}`).then((r) => jsonOrThrow(r));
  }
  upsertProfile(vehicleId: string, input: ProfileInput): Promise<VehicleProfile> {
    return fetchWithAuth(`${BASE}/fleet/profiles/${vehicleId}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }).then((r) => jsonOrThrow(r));
  }

  // catalog
  listDimensions(): Promise<ProductDimension[]> {
    return fetchWithAuth(`${BASE}/catalog/dimensions`).then((r) => jsonOrThrow(r));
  }
  listProducts(): Promise<EffectiveProduct[]> {
    return fetchWithAuth(`${BASE}/catalog/products`).then((r) => jsonOrThrow(r));
  }

  // load
  optimizeLoad(req: OptimizeRequest): Promise<LoadPlan> {
    return fetchWithAuth(`${BASE}/load/optimize`, {
      method: 'POST',
      body: JSON.stringify(req),
    }).then((r) => jsonOrThrow(r));
  }
  getLoadPlan(id: string): Promise<LoadPlan> {
    return fetchWithAuth(`${BASE}/load/${id}`).then((r) => jsonOrThrow(r));
  }

  // routing
  planRoute(req: PlanRequest): Promise<RoutePlan> {
    return fetchWithAuth(`${BASE}/routing/plan`, {
      method: 'POST',
      body: JSON.stringify(req),
    }).then((r) => jsonOrThrow(r));
  }
  getRoutePlan(id: string): Promise<RoutePlan> {
    return fetchWithAuth(`${BASE}/routing/plan/${id}`).then((r) => jsonOrThrow(r));
  }
  approveRoutePlan(id: string): Promise<RoutePlan> {
    return fetchWithAuth(`${BASE}/routing/plan/${id}/approve`, { method: 'POST' }).then((r) =>
      jsonOrThrow(r),
    );
  }

  // compliance
  listRestrictedPoints(): Promise<RestrictedPoint[]> {
    return fetchWithAuth(`${BASE}/compliance/restricted-points`).then((r) => jsonOrThrow(r));
  }
  checkRoute(req: RouteCheckRequest): Promise<RouteCheckResult> {
    return fetchWithAuth(`${BASE}/compliance/check-route`, {
      method: 'POST',
      body: JSON.stringify(req),
    }).then((r) => jsonOrThrow(r));
  }
  createRestrictedPoint(input: RestrictedPointInput): Promise<RestrictedPoint> {
    return fetchWithAuth(`${BASE}/compliance/restricted-points`, {
      method: 'POST',
      body: JSON.stringify(input),
    }).then((r) => jsonOrThrow(r));
  }
}

export const aiLmService = new AiLmService();
