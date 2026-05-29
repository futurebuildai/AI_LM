import { LitElement, html, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { icon } from '../lib/icons.ts';
import { Map, Route, CheckCircle2, AlertTriangle } from 'lucide';
import {
  aiLmService,
  type RoutePlan,
  type RouteCheckResult,
} from '../services/aiLmService.ts';
import '../components/routing/RouteMap.ts';

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

@customElement('ailm-dispatch-board')
export class DispatchBoard extends LitElement {
  createRenderRoot() { return this; }

  @state() private _date = today();
  @state() private _plan: RoutePlan | null = null;
  @state() private _check: RouteCheckResult | null = null;
  @state() private _loading = false;
  @state() private _approving = false;
  @state() private _error = '';

  private async _runPlan() {
    this._loading = true;
    this._error = '';
    this._check = null;
    try {
      this._plan = await aiLmService.planRoute({ date: this._date });
      await this._runCompliance();
    } catch (err) {
      this._error = err instanceof Error ? err.message : String(err);
      this._plan = null;
    } finally {
      this._loading = false;
    }
  }

  private async _runCompliance() {
    if (!this._plan || this._plan.stops.length === 0) return;
    const totalWeight = this._plan.stops.reduce((sum, s) => sum + s.weight_lbs, 0);
    try {
      this._check = await aiLmService.checkRoute({
        route: this._plan.stops.map((s) => ({ lat: s.lat, lng: s.lng })),
        load: {
          gross_weight_lbs: Math.round(totalWeight),
          max_axle_lbs: Math.round(totalWeight / 2),
          height_in: 162,
        },
      });
    } catch {
      // Non-fatal — the route still renders without flags.
      this._check = null;
    }
  }

  private async _approve() {
    if (!this._plan) return;
    this._approving = true;
    this._error = '';
    try {
      this._plan = await aiLmService.approveRoutePlan(this._plan.id);
    } catch (err) {
      this._error = err instanceof Error ? err.message : String(err);
    } finally {
      this._approving = false;
    }
  }

  private _statusBanner() {
    if (!this._check) return nothing;
    const map = {
      PASS: { cls: 'bg-gable-green/10 border-gable-green/30 text-gable-green', label: 'Route clear — no restricted-point violations' },
      WARN: { cls: 'bg-amber-warn/10 border-amber-warn/30 text-amber-warn', label: 'Route has advisory flags — review before dispatch' },
      FAIL: { cls: 'bg-safety-red/10 border-safety-red/30 text-safety-red', label: 'Route violates restricted-point limits' },
    }[this._check.status];
    return html`
      <div class="flex items-center gap-2 px-4 py-2.5 rounded-lg border text-sm font-medium ${map.cls}">
        ${icon(this._check.status === 'PASS' ? CheckCircle2 : AlertTriangle, 18)}
        <span>${map.label}</span>
        ${this._check.flags.length > 0
          ? html`<span class="font-mono ml-auto">${this._check.flags.length} flag(s)</span>`
          : nothing}
      </div>
    `;
  }

  render() {
    const plan = this._plan;
    return html`
      <div class="space-y-6 max-w-[1600px]">
        <header class="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 class="text-2xl font-semibold flex items-center gap-2">${icon(Map, 24, 'text-gable-green')} Dispatch Board</h1>
            <p class="text-sm text-zinc-400 mt-1">Pre-optimized daily routes from confirmed GableLBM orders.</p>
          </div>
          <div class="flex items-end gap-3">
            <label class="flex flex-col gap-1 text-xs text-zinc-400">
              Delivery date
              <input
                type="date"
                .value=${this._date}
                @change=${(e: Event) => { this._date = (e.target as HTMLInputElement).value; }}
                class="bg-slate-steel border border-white/10 rounded-lg px-3 py-2 text-sm font-mono text-white focus:outline-none focus:ring-1 focus:ring-gable-green/50"
              />
            </label>
            <button
              @click=${this._runPlan}
              ?disabled=${this._loading}
              class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all disabled:opacity-50"
            >
              ${icon(Route, 18)} ${this._loading ? 'Optimizing…' : 'Build Route'}
            </button>
          </div>
        </header>

        ${this._error
          ? html`<div class="px-4 py-2.5 rounded-lg border border-safety-red/30 bg-safety-red/10 text-safety-red text-sm">${this._error}</div>`
          : nothing}

        ${this._statusBanner()}

        <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div class="lg:col-span-2">
            <ailm-route-map
              .stops=${plan?.stops ?? []}
              .flags=${this._check?.flags ?? []}
            ></ailm-route-map>
          </div>

          <div class="space-y-4">
            <div class="glass-card rounded-xl p-4">
              <h2 class="text-sm font-semibold text-zinc-300 mb-3">Plan Summary</h2>
              ${plan
                ? html`
                    <dl class="space-y-2 text-sm">
                      <div class="flex justify-between"><dt class="text-zinc-400">Status</dt><dd class="font-mono ${plan.status === 'APPROVED' ? 'text-gable-green' : 'text-blueprint-blue'}">${plan.status}</dd></div>
                      <div class="flex justify-between"><dt class="text-zinc-400">Stops</dt><dd class="font-mono">${plan.stops.length}</dd></div>
                      <div class="flex justify-between"><dt class="text-zinc-400">Distance</dt><dd class="font-mono">${plan.total_distance_mi.toFixed(1)} mi</dd></div>
                      <div class="flex justify-between"><dt class="text-zinc-400">Drive time</dt><dd class="font-mono">${plan.total_duration_min.toFixed(0)} min</dd></div>
                    </dl>
                    <button
                      @click=${this._approve}
                      ?disabled=${this._approving || plan.status === 'APPROVED'}
                      class="mt-4 w-full flex items-center justify-center gap-2 border border-gable-green/40 text-gable-green font-semibold px-4 py-2 rounded-lg hover:bg-gable-green/10 transition-all disabled:opacity-40"
                    >
                      ${icon(CheckCircle2, 18)} ${plan.status === 'APPROVED' ? 'Approved' : this._approving ? 'Writing back…' : 'Approve & Push to GableLBM'}
                    </button>
                  `
                : html`<p class="text-sm text-zinc-500">No plan yet. Pick a date and build a route.</p>`}
            </div>

            ${plan && plan.stops.length > 0
              ? html`
                  <div class="glass-card rounded-xl p-4">
                    <h2 class="text-sm font-semibold text-zinc-300 mb-3">Stop Sequence</h2>
                    <ol class="space-y-1.5">
                      ${plan.stops.map(
                        (s) => html`
                          <li class="flex items-center gap-3 text-sm">
                            <span class="h-6 w-6 shrink-0 rounded-full bg-gable-green/15 text-gable-green font-mono text-xs flex items-center justify-center">${s.sequence}</span>
                            <span class="flex-1 truncate text-zinc-300">${s.address || s.order_id}</span>
                            <span class="font-mono text-xs text-zinc-500">${s.weight_lbs.toLocaleString()} lb</span>
                          </li>
                        `,
                      )}
                    </ol>
                  </div>
                `
              : nothing}
          </div>
        </div>
      </div>
    `;
  }
}
