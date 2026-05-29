import { LitElement, html } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import type { LoadPlan } from '../../services/aiLmService.ts';

export interface BedDims {
  length_in: number;
  width_in: number;
  height_in: number;
}

// Color-codes placed boxes by axle group so yard staff can see at a glance how
// each item contributes to a given axle's load. Index = axle number (1-based).
const AXLE_COLORS = [0x38bdf8, 0x00ffa3, 0xfbbf24, 0xf43f5e, 0xa78bfa, 0xfb923c];

/**
 * <ailm-load-3d-visualizer> — renders the trailer bed and placed boxes in 3D
 * using Three.js. Coordinates from the solver are inches from the front-left-
 * floor corner (X=length, Y=width, Z=height). Three.js uses Y-up, so we map
 * solver (x,y,z) → three (x, z, y) and scale inches → feet for a sane camera.
 */
@customElement('ailm-load-3d-visualizer')
export class Load3DVisualizer extends LitElement {
  createRenderRoot() { return this; }

  @property({ attribute: false }) plan: LoadPlan | null = null;
  @property({ attribute: false }) bed: BedDims | null = null;

  private _scene?: THREE.Scene;
  private _camera?: THREE.PerspectiveCamera;
  private _renderer?: THREE.WebGLRenderer;
  private _controls?: OrbitControls;
  private _frame = 0;
  private _resizeObserver?: ResizeObserver;
  // Shared digital-twin scaling contract: 1 inch = 1/12 Three.js world unit.
  // GableLBM's PIM preview (<gable-product-twin-3d>) uses the identical factor so
  // a product renders at matching world-unit size in both the PIM and here next
  // to the truck bed. Do not change one without the other.
  private readonly _scale = 1 / 12; // inches → feet

  firstUpdated() {
    this._initScene();
    this._rebuild();
  }

  updated(changed: Map<string, unknown>) {
    if ((changed.has('plan') || changed.has('bed')) && this._scene) {
      this._rebuild();
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    cancelAnimationFrame(this._frame);
    this._resizeObserver?.disconnect();
    this._controls?.dispose();
    this._renderer?.dispose();
  }

  private get _canvasHost(): HTMLElement | null {
    return this.querySelector('#three-host');
  }

  private _initScene() {
    const host = this._canvasHost;
    if (!host) return;

    const width = host.clientWidth || 640;
    const height = host.clientHeight || 420;

    this._scene = new THREE.Scene();
    this._scene.background = new THREE.Color(0x0a0b10);

    this._camera = new THREE.PerspectiveCamera(50, width / height, 0.1, 1000);
    this._camera.position.set(18, 14, 18);

    this._renderer = new THREE.WebGLRenderer({ antialias: true });
    this._renderer.setSize(width, height);
    this._renderer.setPixelRatio(window.devicePixelRatio);
    host.appendChild(this._renderer.domElement);

    this._controls = new OrbitControls(this._camera, this._renderer.domElement);
    this._controls.enableDamping = true;
    this._controls.dampingFactor = 0.08;

    this._scene.add(new THREE.AmbientLight(0xffffff, 0.6));
    const dir = new THREE.DirectionalLight(0xffffff, 0.8);
    dir.position.set(10, 20, 10);
    this._scene.add(dir);

    this._resizeObserver = new ResizeObserver(() => this._onResize());
    this._resizeObserver.observe(host);

    const animate = () => {
      this._frame = requestAnimationFrame(animate);
      this._controls?.update();
      if (this._renderer && this._scene && this._camera) {
        this._renderer.render(this._scene, this._camera);
      }
    };
    animate();
  }

  private _onResize() {
    const host = this._canvasHost;
    if (!host || !this._renderer || !this._camera) return;
    const width = host.clientWidth;
    const height = host.clientHeight;
    if (width === 0 || height === 0) return;
    this._renderer.setSize(width, height);
    this._camera.aspect = width / height;
    this._camera.updateProjectionMatrix();
  }

  /** Clears placement/bed meshes and rebuilds them from current props. */
  private _rebuild() {
    if (!this._scene) return;

    // Remove previously built objects (keep lights).
    const removable = this._scene.children.filter((c) => c.userData.built);
    removable.forEach((c) => this._scene!.remove(c));

    const bed = this.bed;
    if (!bed) return;

    const s = this._scale;
    const bedL = bed.length_in * s;
    const bedW = bed.width_in * s;

    // Ground grid centered under the bed.
    const grid = new THREE.GridHelper(Math.max(bedL, bedW) * 1.6, 24, 0x1e2029, 0x161821);
    grid.userData.built = true;
    this._scene.add(grid);

    // Trailer bed: a translucent slab + wireframe outline. We center the whole
    // scene by translating everything so the bed's front-left corner sits at
    // (-bedL/2, 0, -bedW/2).
    const offX = -bedL / 2;
    const offZ = -bedW / 2;

    const bedGeo = new THREE.BoxGeometry(bedL, 0.15, bedW);
    const bedMat = new THREE.MeshStandardMaterial({ color: 0x252836, transparent: true, opacity: 0.7 });
    const bedMesh = new THREE.Mesh(bedGeo, bedMat);
    bedMesh.position.set(offX + bedL / 2, -0.075, offZ + bedW / 2);
    bedMesh.userData.built = true;
    this._scene.add(bedMesh);

    const railH = (bed.height_in * s) || 4;
    const cageGeo = new THREE.BoxGeometry(bedL, railH, bedW);
    const cage = new THREE.LineSegments(
      new THREE.EdgesGeometry(cageGeo),
      new THREE.LineBasicMaterial({ color: 0x00ffa3, transparent: true, opacity: 0.35 }),
    );
    cage.position.set(offX + bedL / 2, railH / 2, offZ + bedW / 2);
    cage.userData.built = true;
    this._scene.add(cage);

    // Placed boxes.
    const placements = this.plan?.placements ?? [];
    for (const p of placements) {
      const l = p.length_in * s;
      const w = p.width_in * s;
      const h = p.height_in * s;
      const geo = new THREE.BoxGeometry(l, h, w);
      const colorIdx = (p.axle_group - 1 + AXLE_COLORS.length) % AXLE_COLORS.length;
      const color = AXLE_COLORS[colorIdx < 0 ? 0 : colorIdx];
      const mat = new THREE.MeshStandardMaterial({ color, transparent: true, opacity: 0.92 });
      const mesh = new THREE.Mesh(geo, mat);
      // Solver gives corner coords; Three boxes are centered.
      mesh.position.set(
        offX + p.x * s + l / 2,
        p.z * s + h / 2,
        offZ + p.y * s + w / 2,
      );
      mesh.userData.built = true;
      this._scene.add(mesh);

      const edges = new THREE.LineSegments(
        new THREE.EdgesGeometry(geo),
        new THREE.LineBasicMaterial({ color: 0x0a0b10, transparent: true, opacity: 0.4 }),
      );
      edges.position.copy(mesh.position);
      edges.userData.built = true;
      this._scene.add(edges);
    }
  }

  render() {
    const empty = !this.plan || (this.plan.placements?.length ?? 0) === 0;
    return html`
      <div class="relative w-full h-[420px] rounded-xl overflow-hidden border border-white/5 bg-deep-space">
        <div id="three-host" class="absolute inset-0"></div>
        ${empty
          ? html`<div class="absolute inset-0 flex items-center justify-center pointer-events-none">
              <span class="text-sm text-zinc-500">Run a load optimization to render the 3D view</span>
            </div>`
          : null}
        <div class="absolute bottom-3 left-3 text-[11px] text-zinc-500 font-mono pointer-events-none">
          drag to orbit · scroll to zoom
        </div>
      </div>
    `;
  }
}
