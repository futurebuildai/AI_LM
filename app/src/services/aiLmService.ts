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
  // Sequenced (multi-stop) plans only:
  order_id?: string;
  stop_sequence?: number;
  step?: number; // 1-based physical packing order
}

export interface AxleLoad {
  axle_number: number;
  weight_lbs: number;
  max_weight_lbs: number;
  utilization: number;
  status: 'PASS' | 'WARN' | 'FAIL';
}

export interface Strap {
  number: number;
  position_in: number; // inches from the bed front
  over_height_in: number;
  required_wll_lbs: number;
}

export interface Securement {
  cargo_weight_lbs: number;
  min_aggregate_wll_lbs: number;
  straps: Strap[];
  recommended_strap: string;
  notes: string[];
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
  max_load_height_in?: number;
  securement?: Securement;
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

// ---- workflow (guided end-to-end dispatch) ------------------------------
export interface AnalyzedLine {
  product_id: string;
  sku: string;
  name?: string;
  quantity: number;
  unit_weight_lbs: number;
  unit_length_in: number;
  unit_width_in: number;
  unit_height_in: number;
  stackable: boolean;
  has_geometry: boolean;
  line_weight_lbs: number;
  line_volume_cuft: number;
}

export interface OrderAnalysis {
  order_id: string;
  customer_name?: string;
  address?: string;
  lat?: number;
  lng?: number;
  lines: AnalyzedLine[];
  total_weight_lbs: number;
  total_volume_cuft: number;
  max_length_in: number;
  piece_count: number;
  shape_profile: 'LONG_LOAD' | 'COMPACT' | 'MIXED';
  routable: boolean;
  issues: string[];
}

export interface WorkflowStop {
  order_id: string;
  sequence: number;
  lat: number;
  lng: number;
  address?: string;
  customer_name?: string;
  weight_lbs: number;
}

export interface ComplianceAction {
  type: 'REROUTE' | 'LOAD_ADJUST' | 'MANUAL_REVIEW';
  description: string;
  resolved: boolean;
}

export interface ComplianceReview {
  status: 'PASS' | 'WARN' | 'FAIL';
  flags: ComplianceFlag[];
  actions: ComplianceAction[];
  detours?: { lat: number; lng: number }[];
  checked_gross_lbs: number;
  checked_max_axle_lbs: number;
  checked_height_in: number;
}

export interface TruckLoad {
  vehicle_id: string;
  vehicle_name: string;
  driver_id?: string;
  driver_name?: string;
  capacity_weight_lbs: number;
  stops: WorkflowStop[];
  total_weight_lbs: number;
  total_distance_mi: number;
  total_duration_min: number;
  bed?: { length_in: number; width_in: number; height_in: number };
  load_plan?: LoadPlan;
  compliance?: ComplianceReview;
}

export type WorkflowStatus = 'ANALYZED' | 'ASSIGNED' | 'PACKED' | 'REVIEWED' | 'PUSHED';

export interface WorkflowPlan {
  id: string;
  plan_date: string;
  status: WorkflowStatus;
  depot_lat: number;
  depot_lng: number;
  orders: OrderAnalysis[];
  loads: TruckLoad[];
  unassigned_orders: WorkflowStop[];
  created_at: string;
  updated_at: string;
}

// ---- auth (staff login) ------------------------------------------------
export interface LoginResponse {
  token: string;
  name: string;
  roles: string[];
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

  // workflow (guided end-to-end dispatch)
  ingestWorkflow(date: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans`, {
      method: 'POST',
      body: JSON.stringify({ date }),
    }).then((r) => jsonOrThrow(r));
  }
  latestWorkflow(date: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/latest?date=${encodeURIComponent(date)}`).then(
      (r) => jsonOrThrow(r),
    );
  }
  getWorkflow(id: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}`).then((r) => jsonOrThrow(r));
  }
  assignWorkflow(id: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}/assign`, { method: 'POST' }).then((r) =>
      jsonOrThrow(r),
    );
  }
  packWorkflow(id: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}/pack`, { method: 'POST' }).then((r) =>
      jsonOrThrow(r),
    );
  }
  resequenceWorkflow(id: string, vehicleId: string, orderIds: string[]): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}/loads/${vehicleId}/sequence`, {
      method: 'PUT',
      body: JSON.stringify({ order_ids: orderIds }),
    }).then((r) => jsonOrThrow(r));
  }
  reviewWorkflow(id: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}/review`, { method: 'POST' }).then((r) =>
      jsonOrThrow(r),
    );
  }
  pushWorkflow(id: string): Promise<WorkflowPlan> {
    return fetchWithAuth(`${BASE}/workflow/plans/${id}/push`, { method: 'POST' }).then((r) =>
      jsonOrThrow(r),
    );
  }
  // auth (staff login backed by GableLBM validate-staff)
  login(email: string): Promise<LoginResponse> {
    return fetchWithAuth(`${BASE}/auth/login`, {
      method: 'POST',
      body: JSON.stringify({ email }),
    }).then((r) => jsonOrThrow(r));
  }
}

export const aiLmService = new AiLmService();
