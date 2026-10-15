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
import {loadMesh} from './lib/mesh'
import {loadCodeEditor} from './components/editor/lazy'
import {setLanguage} from './i18n'
// El catálogo real del lenguaje (el mismo que embebe Go): autocompletado y
// hover del banco con los textos de verdad.
import catalog from '../../assets/scad/catalog.json'

const q = new URLSearchParams(location.search)
const view = q.get('view') || 'main'
const lang = q.get('lang') === 'es' ? 'es' : 'en'
const theme = q.get('theme') || 'dark'
const platform = q.get('platform') || 'darwin'
const es = lang === 'es'

type Scenario = 'ok' | 'error' | 'missing' | 'empty'
const VIEWS: Record<string, {scenario: Scenario; shot?: ShotOptions}> = {
    main: {scenario: 'ok'},
    viewer: {scenario: 'ok', shot: {viewer: {view: 'iso', edges: true}}},
    'viewer-wireframe': {scenario: 'ok', shot: {viewer: {view: 'iso', wireframe: true, color: 4}}},
    'viewer-split': {scenario: 'ok', shot: {viewer: {view: 'iso', split: true, splitView: 'bottom', color: 4}}},
    'viewer-top': {scenario: 'ok', shot: {viewer: {view: 'top', ortho: true, edges: true}}},
    'render-error': {scenario: 'error', shot: {select: '004-usb-c-cutout.scad'}},
    'render-missing': {scenario: 'missing'},
    editor: {scenario: 'ok'},
    'editor-edit': {scenario: 'ok', shot: {editor: {mode: 'edit'}}},
    'editor-search': {scenario: 'ok', shot: {editor: {mode: 'search'}}},
    problems: {scenario: 'error', shot: {select: '004-usb-c-cutout.scad', bottomTab: 'problems'}},
    'save-checkpoint': {
        scenario: 'ok',
        shot: {
            editor: {
                mode: 'save',
                label: es ? 'Tornillos de la tapa' : 'Lid screws',
                description: es ? 'Cuatro tornillos M2.5 en vez de dos, coloreados' : 'Four M2.5 screws instead of two, colored',
            },
        },
    },
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
lip    = 1.2;

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

// ${es ? 'tornillos M2.5 de la tapa' : 'M2.5 lid screws'}
for (p = [[6, 6], [width - 6, depth - 6]])
    translate([p.x, p.y, height]) cylinder(d = 2.5, h = 4);

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

// Clave de malla con la forma real (64 hex). En el banco /mesh/<clave>.stl
// se resuelve a un STL de ejemplo servido por Vite (ver fetch más abajo).
const fakeKey = (file: string) => (file.slice(0, 3) + 'e1c9a4b7d2f05a68'.repeat(4)).slice(0, 64)
const meshDir = platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\ScadLab\\cache\\meshes\\' : '/Users/maker/Library/Application Support/ScadLab/cache/meshes/'

const okResult = (file: string, ms: number, hit: boolean, warnings = 0) => ({
    fileName: file,
    status: 'ok',
    meshPath: `${meshDir}${fakeKey(file)}.stl`,
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
        {severity: 'error', line: 23, col: 0, file: '', message: 'Parser error: syntax error'},
        {severity: 'warning', line: 13, col: 0, file: '', message: es ? 'Se ignora la variable desconocida "usb_w"' : 'Ignoring unknown variable "usb_w"'},
    ],
    log: 'Parsing design (AST generation)...\nWARNING: Ignoring unknown variable "usb_w" in file 004-usb-c-cutout.scad, line 13\nERROR: Parser error: syntax error in file 004-usb-c-cutout.scad, line 23\n',
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

// Lo que devolvería LintCheckpointSource (backend/scad) sobre cada código:
// las líneas coinciden con los fuentes de arriba.
const diag = (severity: string, line: number, col: number, endCol: number, code: string, msg: [string, string]) => ({
    severity, line, col, endLine: line, endCol, code, source: code === 'syntax' ? 'openscad' : 'scadlab', message: es ? msg[0] : msg[1],
})
const lintFor = (file: string) =>
    file === '004-usb-c-cutout.scad'
        ? [
              diag('warning', 2, 9, 30, 'SL005', [
                  'un checkpoint no debe incluir a otro (003-vent-slots.scad): cada checkpoint es autocontenido; copiá lo que necesites',
                  'a checkpoint must not include another one (003-vent-slots.scad): each checkpoint is self-contained; copy what you need',
              ]),
              diag('info', 7, 1, 7, 'SL009', [
                  'parámetro «port_z» del Customizer sin rango: agregá // [min:paso:max] al final de la línea',
                  "Customizer parameter 'port_z' has no range: add // [min:step:max] at the end of the line",
              ]),
              diag('error', 23, 1, 0, 'syntax', ['Parser error: syntax error', 'Parser error: syntax error']),
          ]
        : [
              diag('info', 7, 1, 4, 'SL009', [
                  'parámetro «lip» del Customizer sin rango: agregá // [min:paso:max] al final de la línea',
                  "Customizer parameter 'lip' has no range: add // [min:step:max] at the end of the line",
              ]),
              diag('warning', 23, 1, 4, 'SL003', [
                  'objeto de primer nivel sin color(): envolvelo en color("…") para que el 3MF multicolor lo asigne a un filamento',
                  'top-level object without color(): wrap it in color("…") so the multicolor 3MF can map it to a filament',
              ]),
          ]

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
    editorIndent: 4, editorFontSize: 13, editorWordWrap: false, editorMinimap: false,
    layout: {sidebarWidth: 264, sidebarCollapsed: false, codeWidth: 440, bottomHeight: 200, dimsCollapsed: view === 'viewer-split'},
    swatches: ['#ff7a1a', '#e7e9ee', '#2b2f38', '#3ccf7a', '#5aa2ff', '#d32f2f'],
}

// Malla de ejemplo: frontend/public/uishot/enclosure.stl, generada con
// OpenSCAD desde la plantilla electronics-enclosure.scad (caja + tapa). Sus
// medidas, calculadas sobre ese STL, son las que devolvería GetMeshInfo.
const SAMPLE_STL = '/uishot/enclosure.stl'
const sampleInfo = {
    triangles: 4176, min: [0, 0, 0], max: [78, 124, 27.6], size: [78, 124, 27.6],
    volumeMm3: 30705.3, areaMm2: 35148, watertight: true,
}
const realFetch = window.fetch.bind(window)
window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url
    return realFetch(url.startsWith('/mesh/') ? SAMPLE_STL : input, init)
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
    GetMeshInfo: () => P(sampleInfo),
    SaveSwatches: () => P(undefined),
    SaveScreenshot: () => P(''),
    SaveLayout: (l: unknown) => P(l),
    LintCheckpointSource: (_id: number, file: string) => P(lintFor(file)),
    FormatScad: (src: string) => P(src),
    SaveAsCheckpoint: () => P('007-lid-screws.scad'),
    GetScadCatalog: () => P(catalog),
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
    BrowserOpenURL: () => {},
}

// WebGL en headless corre en SwiftShader, en otro proceso: la foto puede
// salir antes de que ese proceso termine de dibujar y el canvas queda vacío.
// Un readPixels obliga a esperar a que termine; se repite unos segundos
// (tiempo virtual) para cubrir también los cuadros que llegan tarde.
let syncs = 0
const syncGL = window.setInterval(() => {
    // Solo los del visor: pedirle un contexto WebGL al canvas 2D de Monaco
    // (la regla de la derecha) lo deja sin contexto 2D y rompe su dibujo.
    for (const c of document.querySelectorAll('canvas')) {
        if (c.closest('.monaco-editor')) continue
        const gl = c.getContext('webgl2') ?? c.getContext('webgl')
        gl?.readPixels(0, 0, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array(4))
    }
    if (++syncs > 30) window.clearInterval(syncGL)
}, 100)

// El visor 3D entra con lazy() y la malla por fetch: Chrome headless no
// espera esas cargas dentro de --virtual-time-budget y la foto saldría con el
// panel vacío. Se precargan módulo y malla (queda en la caché LRU) antes de
// montar.
// El editor (Monaco) también entra con lazy(): mismo motivo. El idioma va
// antes, porque decide con qué textos carga Monaco sus widgets.
setLanguage(lang)
Promise.all([import('./components/viewer/Viewer'), loadCodeEditor(), loadMesh(okResult(selectedFile, 0, false).meshPath)])
    .catch(() => {})
    .then(() =>
        createRoot(document.getElementById('root')!).render(
            <StrictMode>
                <App shot={cfg.shot} />
            </StrictMode>,
        ),
    )
