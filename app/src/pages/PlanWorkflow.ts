import { LitElement, html, nothing, type TemplateResult } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { icon } from '../lib/icons.ts';
import {
  Sparkles,
  ClipboardList,
  Truck,
  Boxes,
  ShieldAlert,
  Send,
  CheckCircle2,
  AlertTriangle,
  ChevronUp,
  ChevronDown,
  Play,
  Pause,
  RotateCcw,
  ArrowRight,
  Link2,
} from 'lucide';
import {
  aiLmService,
  type WorkflowPlan,
  type WorkflowStatus,
  type TruckLoad,
  type OrderAnalysis,
} from '../services/aiLmService.ts';
import '../components/load/Load3DVisualizer.ts';
import '../components/routing/RouteMap.ts';

// Per-stop accent palette — keep in sync with STOP_COLORS in Load3DVisualizer.
const STOP_HEX = ['#00FFA3', '#38BDF8', '#FBBF24', '#A78BFA', '#F472B6', '#FB923C'];

const STEPS = [
  { n: 1, title: 'Ingest & Analyze', icon: ClipboardList },
  { n: 2, title: 'Assign Trucks', icon: Truck },
  { n: 3, title: 'Pack Loads', icon: Boxes },
  { n: 4, title: 'Route Review', icon: ShieldAlert },
  { n: 5, title: 'Manifest & Push', icon: Send },
] as const;

const STATUS_RANK: Record<WorkflowStatus, number> = {
  ANALYZED: 1,
  ASSIGNED: 2,
  PACKED: 3,
  REVIEWED: 4,
  PUSHED: 5,
};

function tomorrow(): string {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  return d.toISOString().slice(0, 10);
}

/**
 * <ailm-plan-workflow> — the single-page guided dispatch workflow. One stepper
 * carries a delivery date from order ingestion through analysis, truck
 * assignment, LIFO 3D packing, restricted-point review (with AI resolution)
 * and the final manifest push to the GableLBM dispatch board.
 */
@customElement('ailm-plan-workflow')
export class PlanWorkflow extends LitElement {
  createRenderRoot() { return this; }

  @state() private _date = tomorrow();
  @state() private _plan: WorkflowPlan | null = null;
  @state() private _step = 1;
  @state() private _busy = '';
  @state() private _error = '';
  @state() private _notice = '';
  @state() private _activeTruck = 0;
  @state() private _stepSlider = -1;
  @state() private _playing = false;
  private _playTimer = 0;

  connectedCallback() {
    super.connectedCallback();
    this._loadLatest();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.clearInterval(this._playTimer);
  }

  // --- data helpers ---------------------------------------------------------

  private get _maxStep(): number {
    if (!this._plan) return 1;
    return Math.min(STATUS_RANK[this._plan.status] + 1, 5);
  }

  private get _truck(): TruckLoad | null {
    return this._plan?.loads[this._activeTruck] ?? null;
  }

  private async _run<T>(label: string, fn: () => Promise<T>, after?: (v: T) => void) {
    this._busy = label;
    this._error = '';
    this._notice = '';
    try {
      const v = await fn();
      after?.(v);
    } catch (err) {
      this._error = err instanceof Error ? err.message : String(err);
    } finally {
      this._busy = '';
    }
  }

  private async _loadLatest() {
    try {
      this._plan = await aiLmService.latestWorkflow(this._date);
      this._step = Math.min(this._maxStep, 5);
    } catch {
      this._plan = null; // no plan yet for this date — start at step 1
    }
  }

  private _setPlan(p: WorkflowPlan, advanceTo?: number) {
    this._plan = p;
    if (advanceTo) this._step = advanceTo;
    if (this._activeTruck >= p.loads.length) this._activeTruck = 0;
    this._stepSlider = -1;
    this._stopPlayback();
  }

  // --- actions ---------------------------------------------------------------

  private _ingest() {
    this._run('ingest', () => aiLmService.ingestWorkflow(this._date), (p) => this._setPlan(p));
  }

  private _assign() {
    if (!this._plan) return;
    this._run('assign', () => aiLmService.assignWorkflow(this._plan!.id), (p) => this._setPlan(p));
  }

  private _pack() {
    if (!this._plan) return;
    this._run('pack', () => aiLmService.packWorkflow(this._plan!.id), (p) => this._setPlan(p));
  }

  private _review() {
    if (!this._plan) return;
    this._run('review', () => aiLmService.reviewWorkflow(this._plan!.id), (p) => this._setPlan(p));
  }

  private _push() {
    if (!this._plan) return;
    this._run('push', () => aiLmService.pushWorkflow(this._plan!.id), (p) => this._setPlan(p));
  }

  private _moveStop(truck: TruckLoad, idx: number, dir: -1 | 1) {
    const ids = truck.stops.map((s) => s.order_id);
    const j = idx + dir;
    if (j < 0 || j >= ids.length) return;
    [ids[idx], ids[j]] = [ids[j], ids[idx]];
    this._run(
      'reseq',
      () => aiLmService.resequenceWorkflow(this._plan!.id, truck.vehicle_id, ids),
      (p) => this._setPlan(p, this._step),
    );
  }

  // --- packing playback -------------------------------------------------------

  private get _totalSteps(): number {
    return this._truck?.load_plan?.placements.length ?? 0;
  }

  private _togglePlayback() {
    if (this._playing) {
      this._stopPlayback();
      return;
    }
    if (this._stepSlider < 0 || this._stepSlider >= this._totalSteps) this._stepSlider = 0;
    this._playing = true;
    // Adaptive speed: whole playback lasts ~15s regardless of piece count.
    const inc = Math.max(1, Math.round(this._totalSteps / 150));
    this._playTimer = window.setInterval(() => {
      if (this._stepSlider >= this._totalSteps) {
        this._stopPlayback();
        return;
      }
      this._stepSlider = Math.min(this._stepSlider + inc, this._totalSteps);
    }, 100);
  }

  private _stopPlayback() {
    this._playing = false;
    window.clearInterval(this._playTimer);
  }

  // --- render -----------------------------------------------------------------

  render() {
    return html`
      <div class="space-y-6 max-w-[1700px]">
        <header class="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 class="text-2xl font-semibold flex items-center gap-2">
              ${icon(Sparkles, 24, 'text-gable-green')} Load Planner
            </h1>
            <p class="text-sm text-zinc-400 mt-1">
              Guided dispatch — ingest a day's orders, assign trucks, pack in 3D, clear route
              restrictions, push to GableLBM.
            </p>
          </div>
          ${this._plan
            ? html`<div class="flex items-center gap-3 text-xs">
                <span class="text-zinc-500">Plan</span>
                <span class="font-mono text-zinc-300">${this._plan.plan_date}</span>
                <span class="font-mono px-2 py-1 rounded border ${this._plan.status === 'PUSHED'
                  ? 'text-gable-green border-gable-green/40 bg-gable-green/10'
                  : 'text-blueprint-blue border-blueprint-blue/40 bg-blueprint-blue/10'}">${this._plan.status}</span>
              </div>`
            : nothing}
        </header>

        ${this._renderStepper()}

        ${this._error
          ? html`<div class="px-4 py-2.5 rounded-lg border border-safety-red/30 bg-safety-red/10 text-safety-red text-sm">${this._error}</div>`
          : nothing}
        ${this._notice
          ? html`<div class="px-4 py-2.5 rounded-lg border border-gable-green/30 bg-gable-green/10 text-gable-green text-sm">${this._notice}</div>`
          : nothing}

        ${this._renderStep()}
      </div>
    `;
  }

  private _renderStepper() {
    return html`
      <div class="glass-card rounded-xl p-2 flex flex-wrap items-center gap-1">
        ${STEPS.map((s, i) => {
          const done = this._plan ? STATUS_RANK[this._plan.status] >= s.n : false;
          const available = s.n <= this._maxStep;
          const active = this._step === s.n;
          return html`
            ${i > 0 ? html`<span class="text-zinc-600 hidden md:inline">${icon(ArrowRight, 14)}</span>` : nothing}
            <button
              @click=${() => { if (available) { this._stopPlayback(); this._step = s.n; } }}
              ?disabled=${!available}
              class="flex items-center gap-2 px-3 py-2 rounded-lg text-sm font-medium transition-all ${active
                ? 'bg-gable-green/15 text-gable-green shadow-[inset_0_0_0_1px_rgba(0,255,163,0.3)]'
                : available
                  ? 'text-zinc-300 hover:bg-white/5'
                  : 'text-zinc-600 cursor-not-allowed'}"
            >
              <span class="h-6 w-6 rounded-full flex items-center justify-center text-xs font-mono ${done
                ? 'bg-gable-green text-deep-space'
                : active
                  ? 'border border-gable-green text-gable-green'
                  : 'border border-zinc-600 text-zinc-500'}">
                ${done ? icon(CheckCircle2, 14) : s.n}
              </span>
              ${icon(s.icon, 16)}
              <span class="hidden lg:inline">${s.title}</span>
            </button>
          `;
        })}
      </div>
    `;
  }

  private _renderStep(): TemplateResult {
    switch (this._step) {
      case 2: return this._renderAssign();
      case 3: return this._renderPack();
      case 4: return this._renderReview();
      case 5: return this._renderPush();
      default: return this._renderIngest();
    }
  }

  // --- step 1: ingest + analysis ----------------------------------------------

  private _renderIngest() {
    const p = this._plan;
    return html`
      <div class="space-y-6">
        <div class="glass-card rounded-xl p-4 flex flex-wrap items-end gap-4">
          <label class="flex flex-col gap-1 text-xs text-zinc-400">
            Delivery date
            <input
              type="date"
              .value=${this._date}
              @change=${(e: Event) => {
                this._date = (e.target as HTMLInputElement).value;
                this._loadLatest();
              }}
              class="bg-slate-steel border border-white/10 rounded-lg px-3 py-2 text-sm font-mono text-white focus:outline-none focus:ring-1 focus:ring-gable-green/50"
            />
          </label>
          <button
            @click=${this._ingest}
            ?disabled=${this._busy !== ''}
            class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all disabled:opacity-50"
          >
            ${icon(ClipboardList, 18)} ${this._busy === 'ingest' ? 'Analyzing…' : p ? 'Re-ingest orders' : 'Ingest orders'}
          </button>
        </div>

        ${p
          ? html`
              <div class="flex items-center justify-between">
                <h2 class="text-sm font-semibold text-zinc-300">
                  ${p.orders.length} order(s) analyzed —
                  <span class="font-mono">${Math.round(p.orders.reduce((s, o) => s + o.total_weight_lbs, 0)).toLocaleString()} lb</span> ·
                  <span class="font-mono">${Math.round(p.orders.reduce((s, o) => s + o.total_volume_cuft, 0)).toLocaleString()} ft³</span>
                </h2>
                <button
                  @click=${() => { this._step = 2; }}
                  ?disabled=${p.orders.length === 0}
                  class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all disabled:opacity-40"
                >
                  Assign trucks ${icon(ArrowRight, 16)}
                </button>
              </div>
              <div class="grid grid-cols-1 lg:grid-cols-2 2xl:grid-cols-3 gap-4">
                ${p.orders.map((o) => this._orderCard(o))}
              </div>
            `
          : html`<p class="text-sm text-zinc-500">
              Pick the delivery date and ingest its confirmed GableLBM orders to begin planning.
            </p>`}
      </div>
    `;
  }

  private _shapeBadge(shape: OrderAnalysis['shape_profile']) {
    const map = {
      LONG_LOAD: 'text-amber-warn border-amber-warn/40 bg-amber-warn/10',
      COMPACT: 'text-gable-green border-gable-green/40 bg-gable-green/10',
      MIXED: 'text-blueprint-blue border-blueprint-blue/40 bg-blueprint-blue/10',
    } as const;
    return html`<span class="text-[10px] font-mono px-1.5 py-0.5 rounded border ${map[shape]}">${shape.replace('_', ' ')}</span>`;
  }

  private _orderCard(o: OrderAnalysis) {
    return html`
      <div class="glass-card rounded-xl p-4 space-y-3">
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0">
            <div class="text-sm font-semibold text-zinc-200 truncate">${o.customer_name || o.order_id}</div>
            <div class="text-xs text-zinc-500 truncate">${o.address || 'no address'}</div>
          </div>
          ${this._shapeBadge(o.shape_profile)}
        </div>
        <dl class="grid grid-cols-4 gap-2 text-center">
          <div><dt class="text-[10px] text-zinc-500 uppercase">Weight</dt><dd class="font-mono text-sm text-zinc-200">${Math.round(o.total_weight_lbs).toLocaleString()}<span class="text-zinc-500 text-[10px]"> lb</span></dd></div>
          <div><dt class="text-[10px] text-zinc-500 uppercase">Volume</dt><dd class="font-mono text-sm text-zinc-200">${o.total_volume_cuft.toFixed(0)}<span class="text-zinc-500 text-[10px]"> ft³</span></dd></div>
          <div><dt class="text-[10px] text-zinc-500 uppercase">Pieces</dt><dd class="font-mono text-sm text-zinc-200">${o.piece_count}</dd></div>
          <div><dt class="text-[10px] text-zinc-500 uppercase">Max len</dt><dd class="font-mono text-sm text-zinc-200">${(o.max_length_in / 12).toFixed(0)}<span class="text-zinc-500 text-[10px]"> ft</span></dd></div>
        </dl>
        <div class="space-y-1">
          ${o.lines.map(
            (l) => html`
              <div class="flex items-center gap-2 text-xs">
                <span class="font-mono text-zinc-400 w-28 truncate shrink-0">${l.sku}</span>
                <span class="flex-1 truncate text-zinc-300">${l.name || ''}</span>
                <span class="font-mono text-zinc-400">×${l.quantity}</span>
                <span class="font-mono text-zinc-500 w-20 text-right">${Math.round(l.line_weight_lbs).toLocaleString()} lb</span>
                ${l.has_geometry ? nothing : html`<span title="no digital-twin geometry">${icon(AlertTriangle, 12, 'text-amber-warn')}</span>`}
              </div>
            `,
          )}
        </div>
        ${o.issues.length > 0
          ? html`<div class="text-xs text-amber-warn flex items-start gap-1.5">${icon(AlertTriangle, 13)} ${o.issues.join(' · ')}</div>`
          : nothing}
      </div>
    `;
  }

  // --- step 2: assign ----------------------------------------------------------

  private _renderAssign() {
    const p = this._plan;
    if (!p) return this._renderIngest();
    const assigned = p.loads.length > 0;
    return html`
      <div class="space-y-6">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <button
            @click=${this._assign}
            ?disabled=${this._busy !== ''}
            class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all disabled:opacity-50"
          >
            ${icon(Truck, 18)} ${this._busy === 'assign' ? 'Assigning…' : assigned ? 'Re-run assignment' : 'Assign orders to trucks'}
          </button>
          ${assigned
            ? html`<button
                @click=${() => { this._step = 3; if (STATUS_RANK[p.status] < 3) this._pack(); }}
                class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all"
              >
                Pack loads ${icon(ArrowRight, 16)}
              </button>`
            : nothing}
        </div>

        ${assigned
          ? html`
              <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
                <div class="lg:col-span-2">
                  <ailm-route-map
                    .loads=${p.loads as never}
                    .depot=${{ lat: p.depot_lat, lng: p.depot_lng }}
                  ></ailm-route-map>
                </div>
                <div class="space-y-4">
                  ${p.unassigned_orders.length > 0
                    ? html`<div class="flex items-start gap-2 px-4 py-2.5 rounded-lg border border-amber-warn/30 bg-amber-warn/10 text-amber-warn text-sm">
                        ${icon(AlertTriangle, 18)}
                        <div>
                          <span class="font-medium">${p.unassigned_orders.length} order(s) unassigned — no truck capacity.</span>
                          <ul class="mt-1 space-y-0.5 text-xs font-mono text-amber-warn/80">
                            ${p.unassigned_orders.map((s) => html`<li>${s.customer_name || s.order_id} · ${s.weight_lbs.toLocaleString()} lb</li>`)}
                          </ul>
                        </div>
                      </div>`
                    : nothing}
                  ${p.loads.map((l, li) => this._loadSummaryCard(l, li))}
                </div>
              </div>
            `
          : html`<p class="text-sm text-zinc-500">Run the assignment to split ${p.orders.filter((o) => o.routable).length} routable order(s) across the live GableLBM fleet.</p>`}
      </div>
    `;
  }

  private _loadSummaryCard(l: TruckLoad, li: number) {
    const color = STOP_HEX[li % STOP_HEX.length];
    return html`
      <div class="glass-card rounded-xl p-4">
        <div class="flex items-center gap-2 mb-1">
          <span class="h-3 w-3 rounded-full shrink-0" style="background:${color};box-shadow:0 0 8px ${color}"></span>
          <h2 class="text-sm font-semibold text-zinc-200 flex-1 truncate">${l.vehicle_name}</h2>
          <span class="font-mono text-xs text-zinc-500">${l.stops.length} stop(s)</span>
        </div>
        <p class="text-xs text-zinc-400 mb-3 pl-5 truncate">
          ${l.driver_name ? html`Driver: <span class="text-zinc-300">${l.driver_name}</span>` : html`<span class="text-amber-warn">No driver assigned</span>`}
        </p>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs mb-3">
          <div class="flex justify-between"><dt class="text-zinc-400">Weight</dt><dd class="font-mono text-zinc-200">${Math.round(l.total_weight_lbs).toLocaleString()} lb</dd></div>
          <div class="flex justify-between"><dt class="text-zinc-400">Capacity</dt><dd class="font-mono text-zinc-200">${l.capacity_weight_lbs.toLocaleString()} lb</dd></div>
          <div class="flex justify-between"><dt class="text-zinc-400">Distance</dt><dd class="font-mono text-zinc-200">${l.total_distance_mi.toFixed(1)} mi</dd></div>
          <div class="flex justify-between"><dt class="text-zinc-400">Drive time</dt><dd class="font-mono text-zinc-200">${l.total_duration_min.toFixed(0)} min</dd></div>
        </dl>
        <ol class="space-y-1.5">
          ${l.stops.map(
            (s) => html`
              <li class="flex items-center gap-3 text-sm">
                <span class="h-6 w-6 shrink-0 rounded-full font-mono text-xs flex items-center justify-center text-deep-space" style="background:${color}">${s.sequence}</span>
                <span class="flex-1 truncate text-zinc-300">${s.customer_name || s.address || s.order_id}</span>
                <span class="font-mono text-xs text-zinc-500">${s.weight_lbs.toLocaleString()} lb</span>
              </li>
            `,
          )}
        </ol>
      </div>
    `;
  }

  // --- step 3: pack -------------------------------------------------------------

  private _truckTabs() {
    const p = this._plan!;
    return html`
      <div class="flex flex-wrap gap-2">
        ${p.loads.map((l, i) => {
          const active = i === this._activeTruck;
          const status = l.compliance?.status ?? l.load_plan?.gvw_status;
          const dot =
            status === 'FAIL' ? '#F43F5E' : status === 'WARN' ? '#FBBF24' : status ? '#00FFA3' : '#71717a';
          return html`
            <button
              @click=${() => { this._activeTruck = i; this._stepSlider = -1; this._stopPlayback(); }}
              class="flex items-center gap-2 px-3 py-2 rounded-lg text-sm font-medium border transition-all ${active
                ? 'border-gable-green/50 bg-gable-green/10 text-gable-green'
                : 'border-white/10 text-zinc-400 hover:text-white hover:bg-white/5'}"
            >
              ${icon(Truck, 15)} ${l.vehicle_name}
              <span class="h-2 w-2 rounded-full" style="background:${dot}"></span>
            </button>
          `;
        })}
      </div>
    `;
  }

  private _renderPack() {
    const p = this._plan;
    if (!p || p.loads.length === 0) return this._renderAssign();
    const t = this._truck;
    const packed = !!t?.load_plan;
    return html`
      <div class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          ${this._truckTabs()}
          <div class="flex items-center gap-2">
            <button
              @click=${this._pack}
              ?disabled=${this._busy !== ''}
              class="flex items-center gap-2 border border-gable-green/40 text-gable-green font-semibold px-4 py-2 rounded-lg hover:bg-gable-green/10 transition-all disabled:opacity-50"
            >
              ${icon(Boxes, 16)} ${this._busy === 'pack' ? 'Packing…' : packed ? 'Re-pack all trucks' : 'Pack all trucks'}
            </button>
            ${packed
              ? html`<button
                  @click=${() => { this._stopPlayback(); this._step = 4; if (STATUS_RANK[p.status] < 4) this._review(); }}
                  class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all"
                >
                  Route review ${icon(ArrowRight, 16)}
                </button>`
              : nothing}
          </div>
        </div>

        ${t && t.load_plan
          ? html`
              ${this._gvwBanner(t)}
              <div class="grid grid-cols-1 xl:grid-cols-3 gap-6">
                <div class="xl:col-span-2 space-y-3">
                  <ailm-load-3d-visualizer
                    .plan=${t.load_plan}
                    .bed=${t.bed ?? null}
                    colorMode="wood"
                    .visibleSteps=${this._stepSlider}
                  ></ailm-load-3d-visualizer>
                  ${this._playbackControls()}
                  ${t.load_plan.unplaced.length > 0
                    ? html`<div class="px-4 py-2 rounded-lg border border-amber-warn/30 bg-amber-warn/10 text-amber-warn text-xs">
                        Did not fit: <span class="font-mono">${t.load_plan.unplaced.join(', ')}</span>
                      </div>`
                    : nothing}
                </div>
                <div class="space-y-4">
                  ${this._stopSequencer(t)}
                  ${this._axleBars(t)}
                  ${this._securementCard(t)}
                </div>
              </div>
            `
          : html`<p class="text-sm text-zinc-500">Pack the trucks to build each 3D load plan.</p>`}
      </div>
    `;
  }

  private _playbackControls() {
    const total = this._totalSteps;
    if (total === 0) return nothing;
    const val = this._stepSlider < 0 ? total : this._stepSlider;
    return html`
      <div class="glass-card rounded-xl px-4 py-3 flex items-center gap-3">
        <button
          @click=${this._togglePlayback}
          class="h-9 w-9 rounded-lg border border-gable-green/40 text-gable-green flex items-center justify-center hover:bg-gable-green/10 transition-all"
          title="${this._playing ? 'Pause' : 'Play packing sequence'}"
        >
          ${icon(this._playing ? Pause : Play, 16)}
        </button>
        <button
          @click=${() => { this._stopPlayback(); this._stepSlider = -1; }}
          class="h-9 w-9 rounded-lg border border-white/10 text-zinc-400 flex items-center justify-center hover:bg-white/5 transition-all"
          title="Show full load"
        >
          ${icon(RotateCcw, 15)}
        </button>
        <input
          type="range"
          min="0"
          max=${total}
          .value=${String(val)}
          @input=${(e: Event) => {
            this._stopPlayback();
            const v = Number((e.target as HTMLInputElement).value);
            this._stepSlider = v >= total ? -1 : v;
          }}
          class="flex-1 accent-[#00FFA3]"
        />
        <span class="font-mono text-xs text-zinc-400 w-24 text-right">
          ${val} / ${total} pcs
        </span>
      </div>
    `;
  }

  private _stopSequencer(t: TruckLoad) {
    return html`
      <div class="glass-card rounded-xl p-4">
        <h2 class="text-sm font-semibold text-zinc-300 mb-1">Route &amp; Pack Order</h2>
        <p class="text-xs text-zinc-500 mb-3">
          Stop 1 delivers first → packed last (rear of bed). Reorder to re-pack.
        </p>
        <ol class="space-y-1.5">
          ${t.stops.map((s, i) => {
            const color = STOP_HEX[(s.sequence - 1) % STOP_HEX.length];
            return html`
              <li class="flex items-center gap-2 text-sm">
                <span class="h-6 w-6 shrink-0 rounded-full font-mono text-xs flex items-center justify-center text-deep-space" style="background:${color}">${s.sequence}</span>
                <span class="flex-1 truncate text-zinc-300">${s.customer_name || s.address || s.order_id}</span>
                <span class="font-mono text-xs text-zinc-500">${s.weight_lbs.toLocaleString()} lb</span>
                <span class="flex flex-col">
                  <button
                    @click=${() => this._moveStop(t, i, -1)}
                    ?disabled=${i === 0 || this._busy !== ''}
                    class="text-zinc-500 hover:text-gable-green disabled:opacity-30" aria-label="Move stop earlier"
                  >${icon(ChevronUp, 14)}</button>
                  <button
                    @click=${() => this._moveStop(t, i, 1)}
                    ?disabled=${i === t.stops.length - 1 || this._busy !== ''}
                    class="text-zinc-500 hover:text-gable-green disabled:opacity-30" aria-label="Move stop later"
                  >${icon(ChevronDown, 14)}</button>
                </span>
              </li>
            `;
          })}
        </ol>
      </div>
    `;
  }

  private _gvwBanner(t: TruckLoad) {
    const lp = t.load_plan!;
    const map = {
      PASS: { cls: 'bg-gable-green/10 border-gable-green/30 text-gable-green', label: 'GVW PASS — within all axle and gross limits' },
      WARN: { cls: 'bg-amber-warn/10 border-amber-warn/30 text-amber-warn', label: 'GVW WARNING — approaching a rated limit' },
      FAIL: { cls: 'bg-safety-red/10 border-safety-red/30 text-safety-red', label: 'GVW FAIL — overweight; redistribute or remove load' },
    }[lp.gvw_status];
    return html`
      <div class="flex items-center gap-2 px-4 py-2.5 rounded-lg border text-sm font-medium ${map.cls}">
        ${icon(lp.gvw_status === 'PASS' ? CheckCircle2 : AlertTriangle, 18)}
        <span>${map.label}</span>
        <span class="font-mono ml-auto">${lp.total_weight_lbs.toLocaleString()} lb gross · load ${(lp.max_load_height_in ?? 0).toFixed(0)}″ tall</span>
      </div>
    `;
  }

  private _axleBars(t: TruckLoad) {
    const lp = t.load_plan!;
    return html`
      <div class="glass-card rounded-xl p-4">
        <h2 class="text-sm font-semibold text-zinc-300 mb-3">Axle Loads</h2>
        <div class="space-y-3">
          ${lp.axle_loads.map((a) => {
            const pct = Math.min(a.utilization * 100, 120);
            const barColor = a.status === 'FAIL' ? 'bg-safety-red' : a.status === 'WARN' ? 'bg-amber-warn' : 'bg-gable-green';
            return html`
              <div>
                <div class="flex justify-between text-xs mb-1">
                  <span class="text-zinc-400">Axle ${a.axle_number}</span>
                  <span class="font-mono ${a.status === 'FAIL' ? 'text-safety-red' : a.status === 'WARN' ? 'text-amber-warn' : 'text-zinc-300'}">
                    ${a.weight_lbs.toLocaleString()} / ${a.max_weight_lbs.toLocaleString()} lb
                  </span>
                </div>
                <div class="h-2.5 w-full rounded-full bg-white/5 overflow-hidden">
                  <div class="h-full ${barColor} transition-all" style="width: ${pct}%"></div>
                </div>
              </div>
            `;
          })}
        </div>
        <div class="mt-3 pt-3 border-t border-white/5 flex justify-between text-xs">
          <span class="text-zinc-400">Balance score</span>
          <span class="font-mono text-blueprint-blue">${(lp.balance_score * 100).toFixed(0)}%</span>
        </div>
      </div>
    `;
  }

  private _securementCard(t: TruckLoad) {
    const sec = t.load_plan?.securement;
    if (!sec) return nothing;
    return html`
      <div class="glass-card rounded-xl p-4">
        <h2 class="text-sm font-semibold text-zinc-300 mb-1 flex items-center gap-2">
          ${icon(Link2, 15, 'text-amber-warn')} Securement
        </h2>
        <p class="text-xs text-zinc-500 mb-3">
          ${sec.straps.length} tie-down(s) · ${sec.recommended_strap} · straps render in the 3D view once the load is complete.
        </p>
        <div class="space-y-1.5 mb-3">
          ${sec.straps.map(
            (st) => html`
              <div class="flex items-center gap-2 text-xs">
                <span class="h-5 w-5 shrink-0 rounded-full bg-amber-warn/15 border border-amber-warn/40 text-amber-warn font-mono flex items-center justify-center">${st.number}</span>
                <span class="text-zinc-300 flex-1">${(st.position_in / 12).toFixed(1)} ft from nose</span>
                <span class="font-mono text-zinc-500">over ${st.over_height_in.toFixed(0)}″ · WLL ${st.required_wll_lbs.toLocaleString()} lb</span>
              </div>
            `,
          )}
        </div>
        <dl class="flex justify-between text-xs border-t border-white/5 pt-2 mb-2">
          <dt class="text-zinc-400">Required aggregate WLL</dt>
          <dd class="font-mono text-zinc-200">${sec.min_aggregate_wll_lbs.toLocaleString()} lb (50% of ${sec.cargo_weight_lbs.toLocaleString()} lb)</dd>
        </dl>
        <ul class="space-y-1 text-[11px] text-zinc-500 list-disc list-inside">
          ${sec.notes.map((n) => html`<li>${n}</li>`)}
        </ul>
      </div>
    `;
  }

  // --- step 4: compliance review --------------------------------------------------

  private _renderReview() {
    const p = this._plan;
    if (!p || p.loads.length === 0) return this._renderAssign();
    const reviewed = p.loads.every((l) => l.compliance);
    const t = this._truck;
    return html`
      <div class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          ${this._truckTabs()}
          <div class="flex items-center gap-2">
            <button
              @click=${this._review}
              ?disabled=${this._busy !== ''}
              class="flex items-center gap-2 border border-gable-green/40 text-gable-green font-semibold px-4 py-2 rounded-lg hover:bg-gable-green/10 transition-all disabled:opacity-50"
            >
              ${icon(ShieldAlert, 16)} ${this._busy === 'review' ? 'Checking routes…' : reviewed ? 'Re-run route review' : 'Run route review'}
            </button>
            ${reviewed
              ? html`<button
                  @click=${() => { this._step = 5; }}
                  class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all"
                >
                  Manifests ${icon(ArrowRight, 16)}
                </button>`
              : nothing}
          </div>
        </div>

        ${t && t.compliance
          ? html`
              ${this._complianceBanner(t)}
              <div class="grid grid-cols-1 xl:grid-cols-3 gap-6">
                <div class="xl:col-span-2">
                  <ailm-route-map
                    .loads=${[t] as never}
                    .flags=${t.compliance.flags}
                    .depot=${{ lat: p.depot_lat, lng: p.depot_lng }}
                    .detours=${t.compliance.detours ?? []}
                  ></ailm-route-map>
                </div>
                <div class="space-y-4">
                  <div class="glass-card rounded-xl p-4">
                    <h2 class="text-sm font-semibold text-zinc-300 mb-3">Checked Load Profile</h2>
                    <dl class="space-y-2 text-sm">
                      <div class="flex justify-between"><dt class="text-zinc-400">Gross weight</dt><dd class="font-mono">${t.compliance.checked_gross_lbs.toLocaleString()} lb</dd></div>
                      <div class="flex justify-between"><dt class="text-zinc-400">Heaviest axle</dt><dd class="font-mono">${t.compliance.checked_max_axle_lbs.toLocaleString()} lb</dd></div>
                      <div class="flex justify-between"><dt class="text-zinc-400">Travel height</dt><dd class="font-mono">${t.compliance.checked_height_in.toFixed(0)}″ (${(t.compliance.checked_height_in / 12).toFixed(1)} ft)</dd></div>
                    </dl>
                  </div>

                  ${t.compliance.actions.length > 0
                    ? html`<div class="glass-card rounded-xl p-4">
                        <h2 class="text-sm font-semibold text-zinc-300 mb-3">AI Resolutions</h2>
                        <ul class="space-y-2">
                          ${t.compliance.actions.map(
                            (a) => html`
                              <li class="flex items-start gap-2 text-xs">
                                ${icon(a.resolved ? CheckCircle2 : AlertTriangle, 14, a.resolved ? 'text-gable-green shrink-0 mt-0.5' : 'text-amber-warn shrink-0 mt-0.5')}
                                <div>
                                  <span class="font-mono text-[10px] px-1.5 py-0.5 rounded border ${a.resolved ? 'text-gable-green border-gable-green/40' : 'text-amber-warn border-amber-warn/40'}">${a.type}</span>
                                  <p class="text-zinc-300 mt-1">${a.description}</p>
                                </div>
                              </li>
                            `,
                          )}
                        </ul>
                      </div>`
                    : nothing}

                  ${t.compliance.flags.length > 0
                    ? html`<div class="glass-card rounded-xl p-4">
                        <h2 class="text-sm font-semibold text-zinc-300 mb-3">Remaining Flags</h2>
                        <ul class="space-y-2">
                          ${t.compliance.flags.map(
                            (f) => html`
                              <li class="text-xs">
                                <div class="flex items-center gap-2">
                                  <span class="font-mono px-1.5 py-0.5 rounded border ${f.severity === 'FAIL' ? 'text-safety-red border-safety-red/40' : 'text-amber-warn border-amber-warn/40'}">${f.severity}</span>
                                  <span class="text-zinc-200 font-medium">${f.point.name}</span>
                                  <span class="text-zinc-500 font-mono ml-auto">${f.distance_mi} mi</span>
                                </div>
                                <p class="text-zinc-400 mt-1">${f.violation}</p>
                              </li>
                            `,
                          )}
                        </ul>
                      </div>`
                    : nothing}
                </div>
              </div>
            `
          : html`<p class="text-sm text-zinc-500">
              Run the route review to check every truck against bridge weight limits and overpass clearances.
              The AI reroutes or re-balances loads automatically where it can.
            </p>`}
      </div>
    `;
  }

  private _complianceBanner(t: TruckLoad) {
    const c = t.compliance!;
    const map = {
      PASS: { cls: 'bg-gable-green/10 border-gable-green/30 text-gable-green', label: `${t.vehicle_name}: route clear — no restricted-point violations` },
      WARN: { cls: 'bg-amber-warn/10 border-amber-warn/30 text-amber-warn', label: `${t.vehicle_name}: advisory flags on route — review before dispatch` },
      FAIL: { cls: 'bg-safety-red/10 border-safety-red/30 text-safety-red', label: `${t.vehicle_name}: route violates restricted-point limits — manual action needed` },
    }[c.status];
    return html`
      <div class="flex items-center gap-2 px-4 py-2.5 rounded-lg border text-sm font-medium ${map.cls}">
        ${icon(c.status === 'PASS' ? CheckCircle2 : AlertTriangle, 18)}
        <span>${map.label}</span>
        ${c.flags.length > 0 ? html`<span class="font-mono ml-auto">${c.flags.length} flag(s)</span>` : nothing}
      </div>
    `;
  }

  // --- step 5: manifest + push ------------------------------------------------------

  private _renderPush() {
    const p = this._plan;
    if (!p || p.loads.length === 0) return this._renderAssign();
    const anyFail = p.loads.some((l) => l.compliance?.status === 'FAIL');
    const pushed = p.status === 'PUSHED';
    const ordersById = new Map(p.orders.map((o) => [o.order_id, o]));
    return html`
      <div class="space-y-4">
        ${pushed
          ? html`<div class="flex items-center gap-2 px-4 py-3 rounded-lg border border-gable-green/30 bg-gable-green/10 text-gable-green text-sm font-medium">
              ${icon(CheckCircle2, 18)}
              Pushed to GableLBM — routes are on the Dispatch Board and packing instructions are live on the Yard “Pack Trucks” surface.
            </div>`
          : html`<div class="flex flex-wrap items-center justify-between gap-3">
              <p class="text-sm text-zinc-400">
                Final review: ${p.loads.length} truck(s), ${p.loads.reduce((s, l) => s + l.stops.length, 0)} stop(s) on ${p.plan_date}.
              </p>
              <button
                @click=${this._push}
                ?disabled=${this._busy !== '' || anyFail}
                title=${anyFail ? 'Resolve all FAIL compliance flags before pushing' : ''}
                class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-5 py-2.5 rounded-lg hover:shadow-glow transition-all disabled:opacity-40"
              >
                ${icon(Send, 18)} ${this._busy === 'push' ? 'Pushing…' : 'Push to GableLBM dispatch'}
              </button>
            </div>`}
        ${anyFail && !pushed
          ? html`<div class="px-4 py-2.5 rounded-lg border border-safety-red/30 bg-safety-red/10 text-safety-red text-sm">
              One or more trucks still FAIL route compliance — go back to Route Review.
            </div>`
          : nothing}

        <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
          ${p.loads.map((l, li) => this._manifestCard(l, li, ordersById))}
        </div>
      </div>
    `;
  }

  private _manifestCard(l: TruckLoad, li: number, ordersById: Map<string, OrderAnalysis>) {
    const color = STOP_HEX[li % STOP_HEX.length];
    return html`
      <div class="glass-card rounded-xl p-5">
        <div class="flex items-center gap-2 mb-1">
          <span class="h-3 w-3 rounded-full" style="background:${color};box-shadow:0 0 8px ${color}"></span>
          <h2 class="text-base font-semibold text-zinc-100 flex-1">${l.vehicle_name}</h2>
          <span class="font-mono text-xs px-2 py-1 rounded border ${l.compliance?.status === 'PASS'
            ? 'text-gable-green border-gable-green/40'
            : l.compliance?.status === 'WARN'
              ? 'text-amber-warn border-amber-warn/40'
              : 'text-safety-red border-safety-red/40'}">${l.compliance?.status ?? '—'}</span>
        </div>
        <p class="text-xs text-zinc-400 mb-4 pl-5">
          Driver: <span class="text-zinc-300">${l.driver_name || 'unassigned'}</span>
          · <span class="font-mono">${Math.round(l.total_weight_lbs).toLocaleString()} lb</span>
          · <span class="font-mono">${l.total_distance_mi.toFixed(1)} mi</span>
          · <span class="font-mono">${l.total_duration_min.toFixed(0)} min</span>
          · <span class="font-mono">${l.load_plan?.placements.length ?? 0} pcs</span>
        </p>
        <ol class="space-y-3">
          ${l.stops.map((s) => {
            const o = ordersById.get(s.order_id);
            return html`
              <li>
                <div class="flex items-center gap-2 text-sm">
                  <span class="h-6 w-6 shrink-0 rounded-full font-mono text-xs flex items-center justify-center text-deep-space" style="background:${color}">${s.sequence}</span>
                  <span class="font-medium text-zinc-200 flex-1 truncate">${s.customer_name || s.order_id}</span>
                  <span class="font-mono text-xs text-zinc-500">${s.weight_lbs.toLocaleString()} lb</span>
                </div>
                <div class="text-xs text-zinc-500 pl-8 truncate">${s.address || ''}</div>
                ${o
                  ? html`<div class="pl-8 mt-1 space-y-0.5">
                      ${o.lines.map(
                        (line) => html`<div class="flex items-center gap-2 text-xs text-zinc-400">
                          <span class="font-mono w-28 truncate shrink-0">${line.sku}</span>
                          <span class="flex-1 truncate">${line.name || ''}</span>
                          <span class="font-mono">×${line.quantity}</span>
                        </div>`,
                      )}
                    </div>`
                  : nothing}
              </li>
            `;
          })}
        </ol>
      </div>
    `;
  }
}
