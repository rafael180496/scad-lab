// Banco de capturas: monta la app entera con los bindings de Wails simulados
// y datos INVENTADOS, para fotografiarla con Chrome headless
// (scripts/uishot.sh) sin abrir la app ni pedir permiso de grabación de
// pantalla. Las capturas de la ayuda (index.html) salen de acá, nunca de una
// instalación real: una de verdad mete rutas, nombres y proyectos del usuario.
//
// Parámetros de la URL:
//   view   escenario (ver VIEWS abajo)
//   lang   en | es
//   theme  dark | light
//   platform darwin | windows
//
// Una vista nueva = una entrada en VIEWS + su fila en scripts/help-shots.sh.
import {StrictMode} from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App, {type ShotOptions} from './App'
import type {SettingsTab} from './components/settings/SettingsDialog'

const q = new URLSearchParams(location.search)
const view = q.get('view') || 'main'
const lang = q.get('lang') === 'es' ? 'es' : 'en'
const theme = q.get('theme') || 'dark'
const platform = q.get('platform') || 'darwin'
const es = lang === 'es'

type Scenario = 'ok' | 'error' | 'missing' | 'empty'
const VIEWS: Record<string, {scenario: Scenario; shot?: ShotOptions}> = {
    main: {scenario: 'ok'},
    'render-error': {scenario: 'error', shot: {select: '004-usb-c-cutout.scad'}},
    'render-missing': {scenario: 'missing'},
    empty: {scenario: 'empty'},
    newworkspace: {scenario: 'empty', shot: {newWorkspace: true}},
}
for (const tab of ['general', 'openscad', 'ai', 'printing', 'skills', 'about'] as SettingsTab[]) {
    VIEWS[`settings-${tab}`] = {scenario: 'ok', shot: {settingsTab: tab}}
}
const cfg = VIEWS[view] ?? VIEWS.main

// Bus de eventos mínimo en lugar del runtime de Wails.
const handlers = new Map<string, Set<(p: unknown) => void>>()
function emit(name: string, payload: unknown) {
    handlers.get(name)?.forEach((h) => h(payload))
}

// Sin transiciones: Chrome headless puede fotografiar a mitad de una.
const noMotion = document.createElement('style')
noMotion.textContent = '*,*::before,*::after{transition:none!important;animation:none!important}'
document.head.appendChild(noMotion)

// ── Datos inventados ────────────────────────────────────────────────────────
const P = <T,>(v: T) => Promise.resolve(v)
const home = platform === 'windows' ? 'C:\\Users\\maker\\Documents\\ScadLab' : '/Users/maker/Documents/ScadLab'

const workspaces =
    cfg.scenario === 'empty'
        ? []
        : [
              {id: 1, name: es ? 'Caja Raspberry Pi 5' : 'Raspberry Pi 5 case', path: `${home}/rpi5-case`, printerId: 'bambu-a1-mini', description: '', lastOpened: 0, createdAt: 0},
              {id: 2, name: es ? 'Soporte de auriculares' : 'Headphone stand', path: `${home}/headphone-stand`, printerId: 'bambu-a1', description: '', lastOpened: 0, createdAt: 0},
              {id: 3, name: es ? 'Organizador Gridfinity' : 'Gridfinity organizer', path: `${home}/gridfinity`, printerId: 'bambu-p1s', description: '', lastOpened: 0, createdAt: 0},
          ]

const cp = (n: number, file: string, parent: string, desc: string) => ({
    id: n, workspaceId: 1, fileName: file, parentFile: parent, label: '', description: desc,
    sessionId: '', collapsed: false, fileMtime: 0, createdAt: 0,
})
const checkpoints = [
    cp(1, '001-base-box.scad', '', es ? 'Caja base 92×64×32' : 'Base box 92×64×32'),
    cp(2, '002-sliding-lid.scad', '001-base-box.scad', es ? 'Tapa deslizante' : 'Sliding lid'),
    cp(3, '003-vent-slots.scad', '002-sliding-lid.scad', es ? 'Ranuras de ventilación' : 'Vent slots'),
    cp(4, '004-usb-c-cutout.scad', '003-vent-slots.scad', es ? 'Recorte USB-C y HDMI' : 'USB-C and HDMI cutouts'),
    cp(5, '005-snap-lid.scad', '002-sliding-lid.scad', es ? 'Rama: tapa a presión' : 'Branch: snap-fit lid'),
    cp(6, '006-two-color-logo.scad', '005-snap-lid.scad', es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS'),
]
const selectedFile = cfg.scenario === 'error' ? '004-usb-c-cutout.scad' : '006-two-color-logo.scad'

const source = `// @checkpoint parent=005-snap-lid.scad desc="${es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS'}"
/* [${es ? 'Dimensiones' : 'Dimensions'}] */
width  = 92;   // [60:1:160]
depth  = 64;   // [40:1:120]
height = 32;   // [16:1:80]
wall   = 2;    // [1.2:0.2:4]

/* [Hidden] */
$fn = $preview ? 32 : 96;

color("#2b2f38") difference() {
    rounded_box([width, depth, height], r = 3);
    translate([wall, wall, wall])
        rounded_box([width - 2*wall, depth - 2*wall, height], r = 2);
}

color("#ff7a1a")
    translate([width/2, depth/2, height - 0.6])
        linear_extrude(0.6) text("Pi 5", size = 12, halign = "center", valign = "center");

module rounded_box(size, r) {
    hull() for (x = [r, size.x - r], y = [r, size.y - r])
        translate([x, y, 0]) cylinder(r = r, h = size.z);
}
`

const brokenSource = `// @checkpoint parent=003-vent-slots.scad desc="${es ? 'Recorte USB-C y HDMI' : 'USB-C and HDMI cutouts'}"
include <003-vent-slots.scad>

/* [${es ? 'Puertos' : 'Ports'}] */
usb_c  = [9.2, 3.4];   // ${es ? 'ancho, alto' : 'width, height'}
hdmi   = [7.5, 3.6];
port_z = 6;

/* [Hidden] */
eps = 0.01;

// usb_w ${es ? 'quedó del checkpoint anterior' : 'is left over from the previous checkpoint'}
echo(usb_w);

color("#2b2f38") difference() {
    enclosure();
    for (i = [0 : 1]) {
        translate([12 + i * 13.5, -eps, port_z])
            cube([usb_c.x, wall + 2*eps, usb_c.y]);
    }
    translate([48, -eps, port_z])
        cube([hdmi.x, wall + 2*eps, hdmi.y]
}
`

const okResult = (file: string, ms: number, hit: boolean, warnings = 0) => ({
    fileName: file,
    status: 'ok',
    meshPath: `~/Library/Application Support/ScadLab/cache/meshes/${file.slice(0, 3)}e1c9a4.stl`,
    cacheHit: hit,
    durationMs: ms,
    diagnostics: Array.from({length: warnings}, (_, i) => ({
        severity: 'warning', line: 14 + i, col: 0, file: '',
        message: es ? 'Se ignora la variable desconocida "lip"' : 'Ignoring unknown variable "lip"',
    })),
    log: 'Parsing design (AST generation)...\nCompiling design (CSG Tree generation)...\nRendering Polygon Mesh using Manifold...\nTotal rendering time: 0:00:00.412\n',
    backend: 'manifold',
    version: '2025.06.01',
})
const errorResult = {
    fileName: '004-usb-c-cutout.scad',
    status: 'error',
    meshPath: '',
    cacheHit: false,
    durationMs: 230,
    diagnostics: [
        {severity: 'error', line: 27, col: 0, file: '', message: 'Parser error: syntax error'},
        {severity: 'warning', line: 12, col: 0, file: '', message: es ? 'Se ignora la variable desconocida "usb_w"' : 'Ignoring unknown variable "usb_w"'},
    ],
    log: 'Parsing design (AST generation)...\nWARNING: Ignoring unknown variable "usb_w" in file 004-usb-c-cutout.scad, line 12\nERROR: Parser error: syntax error in file 004-usb-c-cutout.scad, line 27\n',
    backend: 'manifold',
    version: '2025.06.01',
}
const missingResult = {
    fileName: selectedFile, status: 'missing', meshPath: '', cacheHit: false, durationMs: 0,
    diagnostics: [], log: '', backend: '', version: '',
}
const renderState: Record<string, unknown> =
    cfg.scenario === 'missing'
        ? {}
        : {
              '001-base-box.scad': okResult('001-base-box.scad', 380, true),
              '002-sliding-lid.scad': okResult('002-sliding-lid.scad', 512, true),
              '003-vent-slots.scad': okResult('003-vent-slots.scad', 690, true, 1),
              '004-usb-c-cutout.scad': errorResult,
              '005-snap-lid.scad': okResult('005-snap-lid.scad', 455, true),
              '006-two-color-logo.scad': okResult('006-two-color-logo.scad', 1240, false),
          }

const printers = [
    ['bambu-a1-mini', 'Bambu Lab A1 mini', 180, 180, 180, 4],
    ['bambu-a1', 'Bambu Lab A1', 256, 256, 256, 4],
    ['bambu-p1s', 'Bambu Lab P1S', 256, 256, 256, 16],
    ['bambu-x1c', 'Bambu Lab X1 Carbon', 256, 256, 256, 16],
    ['bambu-h2d', 'Bambu Lab H2D', 350, 320, 325, 24],
].map(([id, name, x, y, z, slots]) => ({id, name, build_volume: {x, y, z}, nozzle_mm: 0.4, multicolor_slots: slots, multicolor_system: 'AMS', enclosed: false, heated_chamber: false, notes: '', verify: []}))

const settings = {
    language: lang, theme, openscadPath: '', renderBackend: 'manifold', workspacesRoot: '',
    defaultPrinter: 'bambu-a1-mini', defaultMaterial: 'pla', infillPercent: 15, defaultAgent: 'claude',
    autoRender: true, renderTimeoutSec: 300,
}

const App_ = {
    GetBootstrap: () => P({version: '0.1.0', platform, dataDir: platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\ScadLab' : '/Users/maker/Library/Application Support/ScadLab', schemaVersion: 1, initError: '', settings}),
    GetSettings: () => P(settings),
    ListPrinters: () => P(printers),
    ListWorkspaces: () => P(workspaces),
    OpenWorkspace: () => P({workspace: workspaces[0], checkpoints, missing: false}),
    RescanWorkspace: () => P({workspace: workspaces[0], checkpoints, missing: false}),
    ReadCheckpoint: (_id: number, file: string) => P(file === '004-usb-c-cutout.scad' ? brokenSource : source),
    EffectiveWorkspacesRoot: () => P(home),
    GetRenderState: () => P(renderState),
    // Igual que el backend: emite render:started / render:finished, que es lo
    // que mira la UI (el valor devuelto solo lo usa quien llama).
    RenderCheckpoint: (id: number, file: string) => {
        const result = cfg.scenario === 'missing' ? missingResult : (renderState[file] ?? okResult(file, 600, false))
        emit('render:started', {workspaceId: id, fileName: file})
        setTimeout(() => emit('render:finished', {workspaceId: id, result}), 50)
        return P(result)
    },
    CancelRender: () => P(undefined),
    DetectTools: () =>
        P([
            {id: 'openscad', found: true, path: '/Applications/OpenSCAD.app/Contents/MacOS/OpenSCAD', version: '2025.06.01'},
            {id: 'claude', found: true, path: '', version: '2.1.300'},
            {id: 'opencode', found: true, path: '', version: '1.4.2'},
            {id: 'codex', found: true, path: '', version: '0.98.0'},
            {id: 'antigravity', found: false, path: '', version: ''},
        ]),
    ListProviders: () =>
        P([
            {id: 'claude', kind: 'cli', name: 'Claude Code', command: 'claude', config: {}, enabled: true, sortOrder: 1},
            {id: 'opencode', kind: 'cli', name: 'opencode', command: 'opencode', config: {}, enabled: true, sortOrder: 2},
            {id: 'codex', kind: 'cli', name: 'Codex', command: 'codex', config: {}, enabled: true, sortOrder: 3},
            {id: 'antigravity', kind: 'cli', name: 'Antigravity', command: 'agy', config: {}, enabled: true, sortOrder: 4},
            {id: 'ollama', kind: 'ollama', name: 'Ollama', command: '', config: {baseUrl: 'http://localhost:11434', model: 'qwen3-coder:30b'}, enabled: true, sortOrder: 5},
            {id: 'openai', kind: 'openai', name: 'OpenAI-compatible', command: '', config: {baseUrl: 'http://localhost:1234/v1', model: '', apiKeyEnv: 'OPENAI_API_KEY'}, enabled: false, sortOrder: 6},
            {id: 'anthropic', kind: 'anthropic', name: 'Anthropic API', command: '', config: {model: 'claude-sonnet-5-5', apiKeyEnv: 'ANTHROPIC_API_KEY'}, enabled: false, sortOrder: 7},
        ]),
    ListMaterials: () =>
        P([
            {id: 'pla', name: 'PLA', densityGCm3: 1.24, costPerKg: 20},
            {id: 'petg', name: 'PETG', densityGCm3: 1.27, costPerKg: 22},
            {id: 'abs', name: 'ABS', densityGCm3: 1.04, costPerKg: 22},
            {id: 'asa', name: 'ASA', densityGCm3: 1.07, costPerKg: 25},
            {id: 'tpu', name: 'TPU', densityGCm3: 1.21, costPerKg: 35},
        ]),
    ListSkills: () =>
        P([
            {name: 'bambu-lab-printers', source: 'builtin', dir: '', enabled: true, description: es ? 'Perfiles de impresoras Bambu Lab y reglas de diseño FDM.' : 'Bambu Lab printer profiles and FDM design rules.'},
            {name: 'openscad-3mf-multicolor', source: 'builtin', dir: '', enabled: true, description: es ? 'Modelos multicolor para el AMS y exportación 3MF.' : 'Multicolor models for the AMS and 3MF export.'},
            {name: 'openscad-core', source: 'builtin', dir: '', enabled: true, description: es ? 'Reglas de modelado paramétrico y del workspace.' : 'Parametric modeling and workspace rules.'},
            {name: 'openscad-libraries', source: 'builtin', dir: '', enabled: false, description: es ? 'BOSL2, MCAD y NopSCADlib.' : 'BOSL2, MCAD and NopSCADlib.'},
            {name: 'openscad-templates', source: 'builtin', dir: '', enabled: true, description: es ? 'Diez plantillas paramétricas listas para imprimir.' : 'Ten print-ready parametric templates.'},
            {name: 'gear-helper', source: 'user', dir: '', enabled: true, description: es ? 'Engranajes helicoidales con el módulo de mi taller.' : 'Helical gears using my workshop module.'},
        ]),
}

// Cualquier binding no simulado devuelve vacío en vez de romper la vista.
;(window as unknown as {go: unknown}).go = {
    main: {App: new Proxy(App_, {get: (o, k: string) => (k in o ? o[k as keyof typeof o] : () => P(null))})},
}
;(window as unknown as {runtime: unknown}).runtime = {
    EventsOnMultiple: (name: string, cb: (p: unknown) => void) => {
        if (!handlers.has(name)) handlers.set(name, new Set())
        handlers.get(name)!.add(cb)
        return () => handlers.get(name)!.delete(cb)
    },
    EventsOff: () => {},
    EventsEmit: () => {},
}

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <App shot={cfg.shot} />
    </StrictMode>,
)
