import { LitElement, html, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { icon } from '../lib/icons.ts';
import { Boxes, Play, CheckCircle2, AlertTriangle, Plus, Trash2 } from 'lucide';
import {
  aiLmService,
  type VehicleProfile,
  type LoadItem,
  type LoadPlan,
  type EffectiveProduct,
} from '../services/aiLmService.ts';
import '../components/load/Load3DVisualizer.ts';
import type { BedDims } from '../components/load/Load3DVisualizer.ts';

// A small starter load so the 3D view has something to show without GableLBM.
function demoItems(): LoadItem[] {
  return [
    { product_id: 'demo-2x4', sku: '2x4-STUD-8FT', quantity: 40, length_in: 96, width_in: 3.5, height_in: 1.5, weight_lbs: 9, stackable: true },
    { product_id: 'demo-osb', sku: 'OSB-7/16-4x8', quantity: 30, length_in: 96, width_in: 48, height_in: 0.4375, weight_lbs: 46, stackable: true },
    { product_id: 'demo-drywall', sku: 'DRYWALL-1/2-4x8', quantity: 24, length_in: 96, width_in: 48, height_in: 0.5, weight_lbs: 52, stackable: true },
  ];
}

@customElement('ailm-yard-load-view')
export class YardLoadView extends LitElement {
  createRenderRoot() { return this; }

  @state() private _profiles: VehicleProfile[] = [];
  @state() private _vehicleId = '';
  @state() private _items: LoadItem[] = demoItems();
  @state() private _plan: LoadPlan | null = null;
  @state() private _loading = false;
  @state() private _error = '';
  @state() private _products: EffectiveProduct[] = [];

  connectedCallback() {
    super.connectedCallback();
    this._loadProfiles();
    this._loadProducts();
  }

  // Pulls the resolved catalog (PIM geometry merged with AI_LM overrides) so the
  // user can build a load from real products. The demo items remain as a dev
  // fallback when the catalog is empty or the PIM is unreachable.
  private async _loadProducts() {
    try {
      this._products = await aiLmService.listProducts();
    } catch (err) {
      // Non-fatal: the manual item editor + demo items still work offline.
      console.warn('Failed to load catalog products:', err);
    }
  }

  private get _missingGeometryCount(): number {
    return this._products.filter((p) => !p.has_geometry).length;
  }

  private async _loadProfiles() {
    try {
      this._profiles = await aiLmService.listProfiles();
      if (this._profiles.length > 0 && !this._vehicleId) {
        this._vehicleId = this._profiles[0].gable_vehicle_id;
      }
    } catch (err) {
      this._error = err instanceof Error ? err.message : String(err);
    }
  }

  private get _selectedProfile(): VehicleProfile | undefined {
    return this._profiles.find((p) => p.gable_vehicle_id === this._vehicleId);
  }

  private get _bed(): BedDims | null {
    const p = this._selectedProfile;
    return p ? { length_in: p.bed_length_in, width_in: p.bed_width_in, height_in: p.bed_height_in } : null;
  }

  private async _optimize() {
    if (!this._vehicleId) {
      this._error = 'Select a vehicle profile first.';
      return;
    }
    this._loading = true;
    this._error = '';
    try {
      this._plan = await aiLmService.optimizeLoad({ vehicle_id: this._vehicleId, items: this._items });
    } catch (err) {
      this._error = err instanceof Error ? err.message : String(err);
      this._plan = null;
    } finally {
      this._loading = false;
    }
  }

  private _updateItem(idx: number, field: keyof LoadItem, value: string) {
    const items = [...this._items];
    const num = field === 'sku' ? value : Number(value);
    items[idx] = { ...items[idx], [field]: num } as LoadItem;
    this._items = items;
  }

  private _addItem() {
    this._items = [
      ...this._items,
      { product_id: `item-${this._items.length + 1}`, sku: 'NEW-SKU', quantity: 1, length_in: 48, width_in: 24, height_in: 12, weight_lbs: 25, stackable: true },
    ];
  }

  // Adds a real catalog product as a load item, carrying its resolved geometry
  // (PIM-canonical or overridden) so it renders as a scaled digital twin.
  private _addProductById(productId: string) {
    const p = this._products.find((x) => x.gable_product_id === productId);
    if (!p) return;
    this._items = [
      ...this._items,
      {
        product_id: p.gable_product_id,
        sku: p.sku,
        quantity: 1,
        length_in: p.length_in,
        width_in: p.width_in,
        height_in: p.height_in,
        weight_lbs: p.weight_lbs,
        stackable: p.stackable,
      },
    ];
  }

  private _removeItem(idx: number) {
    this._items = this._items.filter((_, i) => i !== idx);
  }

  private _gvwBanner() {
    if (!this._plan) return nothing;
    const map = {
      PASS: { cls: 'bg-gable-green/10 border-gable-green/30 text-gable-green', label: 'GVW PASS — within all axle and gross limits' },
      WARN: { cls: 'bg-amber-warn/10 border-amber-warn/30 text-amber-warn', label: 'GVW WARNING — approaching a rated limit' },
      FAIL: { cls: 'bg-safety-red/10 border-safety-red/30 text-safety-red', label: 'GVW FAIL — overweight; redistribute or remove load' },
    }[this._plan.gvw_status];
    return html`
      <div class="flex items-center gap-2 px-4 py-2.5 rounded-lg border text-sm font-medium ${map.cls}">
        ${icon(this._plan.gvw_status === 'PASS' ? CheckCircle2 : AlertTriangle, 18)}
        <span>${map.label}</span>
        <span class="font-mono ml-auto">${this._plan.total_weight_lbs.toLocaleString()} lbs total</span>
      </div>
    `;
  }

  private _axleBars() {
    if (!this._plan) return nothing;
    return html`
      <div class="glass-card rounded-xl p-4">
        <h2 class="text-sm font-semibold text-zinc-300 mb-3">Axle Loads</h2>
        <div class="space-y-3">
          ${this._plan.axle_loads.map((a) => {
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
          <span class="font-mono text-blueprint-blue">${(this._plan.balance_score * 100).toFixed(0)}%</span>
        </div>
      </div>
    `;
  }

  render() {
    return html`
      <div class="space-y-6 max-w-[1600px]">
        <header class="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 class="text-2xl font-semibold flex items-center gap-2">${icon(Boxes, 24, 'text-gable-green')} Load Builder</h1>
            <p class="text-sm text-zinc-400 mt-1">Optimize material placement and balance weight across axles.</p>
          </div>
          <div class="flex items-end gap-3">
            <label class="flex flex-col gap-1 text-xs text-zinc-400">
              Vehicle
              <select
                .value=${this._vehicleId}
                @change=${(e: Event) => { this._vehicleId = (e.target as HTMLSelectElement).value; }}
                class="bg-slate-steel border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-gable-green/50 min-w-[220px]"
              >
                ${this._profiles.length === 0
                  ? html`<option value="">No profiles — seed the DB</option>`
                  : this._profiles.map((p) => html`<option value=${p.gable_vehicle_id}>${p.name}</option>`)}
              </select>
            </label>
            <button
              @click=${this._optimize}
              ?disabled=${this._loading}
              class="flex items-center gap-2 bg-gable-green text-deep-space font-semibold px-4 py-2 rounded-lg hover:shadow-glow transition-all disabled:opacity-50"
            >
              ${icon(Play, 18)} ${this._loading ? 'Solving…' : 'Optimize Load'}
            </button>
          </div>
        </header>

        ${this._error
          ? html`<div class="px-4 py-2.5 rounded-lg border border-safety-red/30 bg-safety-red/10 text-safety-red text-sm">${this._error}</div>`
          : nothing}

        ${this._gvwBanner()}

        <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div class="lg:col-span-2 space-y-4">
            <ailm-load-3d-visualizer .plan=${this._plan} .bed=${this._bed}></ailm-load-3d-visualizer>
            ${this._plan && this._plan.unplaced.length > 0
              ? html`<div class="px-4 py-2 rounded-lg border border-amber-warn/30 bg-amber-warn/10 text-amber-warn text-xs">
                  ${this._plan.unplaced.length} item(s) did not fit: <span class="font-mono">${this._plan.unplaced.join(', ')}</span>
                </div>`
              : nothing}
          </div>

          <div class="space-y-4">
            ${this._axleBars()}
          </div>
        </div>

        <div class="glass-card rounded-xl p-4">
          <div class="flex flex-wrap items-center justify-between gap-3 mb-3">
            <h2 class="text-sm font-semibold text-zinc-300">Load Items</h2>
            <div class="flex items-center gap-3">
              ${this._products.length > 0
                ? html`<select
                    @change=${(e: Event) => {
                      const sel = e.target as HTMLSelectElement;
                      this._addProductById(sel.value);
                      sel.value = '';
                    }}
                    class="bg-deep-space border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none focus:ring-1 focus:ring-gable-green/50 min-w-[220px]"
                  >
                    <option value="">Add a product…</option>
                    ${this._products.map(
                      (p) => html`<option value=${p.gable_product_id}>
                        ${p.has_geometry ? '' : '⚠ '}${p.sku} — ${p.name}
                      </option>`,
                    )}
                  </select>`
                : nothing}
              <button @click=${this._addItem} class="flex items-center gap-1.5 text-xs text-gable-green hover:underline">
                ${icon(Plus, 14)} Add blank
              </button>
            </div>
          </div>
          ${this._missingGeometryCount > 0
            ? html`<div class="flex items-center gap-2 mb-3 px-3 py-2 rounded-lg border border-amber-warn/30 bg-amber-warn/10 text-amber-warn text-xs">
                ${icon(AlertTriangle, 14)}
                ${this._missingGeometryCount} catalog product(s) have no PIM geometry yet — set dimensions in GableLBM to load them as scaled twins.
              </div>`
            : nothing}
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="text-xs text-zinc-500 border-b border-white/5">
                  <th class="text-left font-medium py-2 pr-3">SKU</th>
                  <th class="text-right font-medium py-2 px-2">Qty</th>
                  <th class="text-right font-medium py-2 px-2">L (in)</th>
                  <th class="text-right font-medium py-2 px-2">W (in)</th>
                  <th class="text-right font-medium py-2 px-2">H (in)</th>
                  <th class="text-right font-medium py-2 px-2">Wt (lb)</th>
                  <th class="py-2 pl-2"></th>
                </tr>
              </thead>
              <tbody>
                ${this._items.map(
                  (it, idx) => html`
                    <tr class="border-b border-white/5">
                      <td class="py-1.5 pr-3">${this._cell(idx, 'sku', it.sku, 'text', 'min-w-[160px]')}</td>
                      <td class="py-1.5 px-2">${this._cell(idx, 'quantity', String(it.quantity), 'number')}</td>
                      <td class="py-1.5 px-2">${this._cell(idx, 'length_in', String(it.length_in), 'number')}</td>
                      <td class="py-1.5 px-2">${this._cell(idx, 'width_in', String(it.width_in), 'number')}</td>
                      <td class="py-1.5 px-2">${this._cell(idx, 'height_in', String(it.height_in), 'number')}</td>
                      <td class="py-1.5 px-2">${this._cell(idx, 'weight_lbs', String(it.weight_lbs), 'number')}</td>
                      <td class="py-1.5 pl-2 text-right">
                        <button @click=${() => this._removeItem(idx)} class="text-zinc-500 hover:text-safety-red" aria-label="Remove item">
                          ${icon(Trash2, 16)}
                        </button>
                      </td>
                    </tr>
                  `,
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    `;
  }

  private _cell(idx: number, field: keyof LoadItem, value: string, type: 'text' | 'number', extra = '') {
    return html`
      <input
        type=${type}
        .value=${value}
        @change=${(e: Event) => this._updateItem(idx, field, (e.target as HTMLInputElement).value)}
        class="bg-deep-space border border-white/10 rounded px-2 py-1 ${type === 'number' ? 'text-right font-mono w-20' : 'text-left'} ${extra} text-white focus:outline-none focus:ring-1 focus:ring-gable-green/50"
      />
    `;
  }
}
