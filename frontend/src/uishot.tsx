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
//   scale  tamaño de la interfaz en % (90–150); por defecto 100
//   keytest 1 = simula ⌘+ ⌘- ⌘0 (interfaz) y ⌘+ con el foco en el editor, la terminal
//          y el chat; el resultado queda en data-keytest
//   dockraw  texto crudo del diseño de paneles guardado (prueba de diseño roto o ajeno)
//   poke   1 = intenta desplazar el documento (scrollTo, focus, scrollIntoView)
//          para comprobar que no se mueve; el resultado queda en data-docfit
//   persist  reset | 1 = «cerrar y abrir»: los bindings de guardado escriben en localStorage
//          (uishot.persist.v1) y GetBootstrap / ListOpenProjects / OpenProject leen de ahí.
//          `reset` parte de cero; recargar con `1` es abrir la app otra vez con lo guardado
//          (scripts/uishot-persist.sh lo maneja por CDP)
//
// Una vista nueva = una entrada en VIEWS + su fila en scripts/help-shots.sh.
import {StrictMode} from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App, {type ShotOptions} from './App'
import type {SettingsTab} from './components/settings/SettingsDialog'
import {loadMesh, loadParts} from './lib/mesh'
import {loadCodeEditor} from './components/editor/lazy'
import {setLanguage} from './i18n'
import {terminalShot} from './components/terminal/shot'
import {shotTools} from './components/dock/dock'
import {printStore, selectPart} from './lib/printSession'
import {editorCommands, editorSession} from './lib/editorSession'
import {installScrollGuard} from './lib/scrollGuard'
import {slugify} from './lib/slug'
// El catálogo real del lenguaje (el mismo que embebe Go): autocompletado y
// hover del banco con los textos de verdad.
import catalog from '../../assets/scad/catalog.json'

const q = new URLSearchParams(location.search)
const view = q.get('view') || 'main'
const lang = q.get('lang') === 'es' ? 'es' : 'en'
const theme = q.get('theme') || 'dark'
const platform = q.get('platform') || 'darwin'
const es = lang === 'es'
// Tamaño de la interfaz en % (UISHOT_SCALE en scripts/uishot.sh).
const scale = [90, 100, 115, 130, 150].includes(Number(q.get('scale'))) ? Number(q.get('scale')) : 100

type Scenario = 'ok' | 'error' | 'missing' | 'empty' | 'wasm'
const VIEWS: Record<string, {scenario: Scenario; shot?: ShotOptions}> = {
    main: {scenario: 'ok'},
    viewer: {scenario: 'ok', shot: {viewer: {view: 'iso', edges: true}}},
    'viewer-wireframe': {scenario: 'ok', shot: {viewer: {view: 'iso', wireframe: true, color: 4}}},
    'viewer-split': {scenario: 'ok', shot: {viewer: {view: 'iso', split: true, splitView: 'bottom', color: 4}}},
    'viewer-top': {scenario: 'ok', shot: {viewer: {view: 'top', ortho: true, edges: true}}},
    'render-error': {scenario: 'error', shot: {select: '004-usb-c-cutout.scad'}},
    'render-missing': {scenario: 'missing'},
    // Motor integrado (S11): sin OpenSCAD instalado, el render sale con el WebAssembly; el aviso del panel y la
    // insignia de la barra de estado. Y Configuración → OpenSCAD con la descarga de OpenSCAD a medias.
    'render-wasm': {scenario: 'wasm', shot: {viewer: {view: 'iso', edges: true}}},
    'settings-openscad-download': {scenario: 'wasm', shot: {settingsTab: 'openscad'}},
    editor: {scenario: 'ok'},
    'editor-edit': {scenario: 'ok', shot: {editor: {mode: 'edit'}}},
    'editor-search': {scenario: 'ok', shot: {editor: {mode: 'search'}}},
    problems: {scenario: 'error', shot: {select: '004-usb-c-cutout.scad', panel: 'problems'}},
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
    // Terminal de agentes (S5, bloque 5.A): una sesión de Claude Code simulada
    // escribiendo un checkpoint, y el menú para retomar una conversación.
    terminal: {scenario: 'ok', shot: {panel: 'agent-1', dock: 'tall'}},
    'terminal-sessions': {scenario: 'ok', shot: {panel: 'agent-1', dock: 'tall'}},
    // Servidor MCP (S6, bloque 6.B): el agente usando las herramientas de ScadLab
    // y el indicador «MCP ✓» de la barra; y Configuración → IA con el estado de
    // los tres archivos del workspace.
    'terminal-mcp': {scenario: 'ok', shot: {panel: 'agent-1', dock: 'tall'}},
    'settings-mcp': {scenario: 'ok', shot: {settingsTab: 'ai'}},
    // Chat con modelos (S5, bloque 5.B): una conversación cuya respuesta trae un
    // bloque OpenSCAD con «Guardar como checkpoint», y el aviso sin proveedores.
    chat: {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-empty': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-new': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-stream': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    // Exportación (S9, tanda 9.B): el diálogo de 3MF con dos colores y sin avisos,
    // y con cinco colores en una A1 mini (admite 4: aviso de colores de más).
    'export-3mf': {scenario: 'ok', shot: {exportDialog: true}},
    'export-3mf-warning': {scenario: 'ok', shot: {exportDialog: true}},
    // Paneles acoplables (S4b): el diseño predeterminado, uno con otra división y
    // un panel flotante, el menú Ver abierto, el diálogo de diseños guardados, y
    // solo el visor con la barra lateral oculta.
    'dock-default': {scenario: 'ok'},
    'dock-custom': {scenario: 'ok', shot: {dock: 'custom', panel: 'chat'}},
    'dock-view-menu': {scenario: 'ok', shot: {viewMenu: 'menu'}},
    'dock-layouts': {scenario: 'ok', shot: {viewMenu: 'layouts'}},
    'dock-hidden': {scenario: 'ok', shot: {dock: 'hidden'}},
    // Minimizar y «Mover a…»: el código a una franja a la derecha y los agentes a su
    // barra abajo; y el menú «…» del código con «Mover a…» abierto.
    'dock-minimized': {scenario: 'ok', shot: {dock: 'minimized', panel: 'viewport'}},
    'dock-move-menu': {scenario: 'ok', shot: {dockMenu: {panel: 'source', submenu: 'move'}}},
    'dock-150': {scenario: 'ok', shot: {panel: 'agent-1'}},
    // Medición e impresión (S7): el visor con las partes de un modelo bicolor y una
    // seleccionada (con su tarjeta), el panel Impresión con la tabla por parte, y una
    // pieza que no cabe en la placa de una A1 mini (contorno rojo y aviso).
    'viewer-parts': {scenario: 'ok', shot: {dock: 'hidden', viewer: {view: 'iso', select: 1}}},
    'print-panel': {scenario: 'ok', shot: {panel: 'print', dock: 'print', viewer: {view: 'iso', select: 1}}},
    'plate-overflow': {scenario: 'ok', shot: {viewer: {view: 'iso'}}},
    // Checkpoints avanzados (S3): el menú contextual de un nodo, la rama activa
    // marcada en el árbol (con el padre activo en 003 y la selección en 006), la
    // papelera y la comparación de un checkpoint con su padre.
    'tree-menu': {scenario: 'ok', shot: {treeMenu: '003-vent-slots.scad'}},
    'tree-active-branch': {scenario: 'ok'},
    trash: {scenario: 'ok', shot: {trash: true}},
    'diff-parent': {scenario: 'ok', shot: {select: '006-two-color-logo.scad', compare: '006-two-color-logo.scad', panel: 'source', dock: 'diff'}},
    // Pestañas de proyecto (S8): tres proyectos abiertos (uno con un render en curso y
    // cambios sin guardar, otro con un agente vivo y su último render fallido), el menú «+»
    // y la barra con poco ancho (se captura a 1024 px).
    'projects-tabs': {scenario: 'ok', shot: {unsavedTabs: [2]}},
    'projects-plus-menu': {scenario: 'ok', shot: {projectsMenu: 'plus', unsavedTabs: [2]}},
    'projects-narrow': {scenario: 'ok', shot: {unsavedTabs: [2]}},
    empty: {scenario: 'empty'},
    // «Nuevo workspace» con la ubicación elegida y la vista previa de la ruta final.
    newworkspace: {scenario: 'empty', shot: {newWorkspace: {name: es ? 'Soporte de auriculares' : 'Headphone stand', parent: '/Users/maker/Documents/Modelos 3D/Impresiones'}}},
    // Barra lateral con nombres larguísimos (workspace y cadena de checkpoints): ver uishot-fit.sh.
    'sidebar-long': {scenario: 'ok'},
    // «Cerrar y abrir» (scripts/uishot-persist.sh): el modelo bicolor, con todos los paneles, y
    // los bindings de guardado escribiendo en localStorage (parámetro persist).
    persist: {scenario: 'ok'},
}
// Apariencia (S10): la pantalla de Configuración → General con su bloque de
// tamaños, y la ventana principal con la interfaz agrandada (scale en la URL).
VIEWS['small'] = {scenario: 'ok', shot: {panel: 'agent-1'}}
VIEWS['appearance-150'] = {scenario: 'ok', shot: {panel: 'agent-1'}}
VIEWS['settings-appearance'] = {scenario: 'ok', shot: {settingsTab: 'general'}}
for (const tab of ['general', 'openscad', 'ai', 'printing', 'skills', 'about'] as SettingsTab[]) {
    VIEWS[`settings-${tab}`] = {scenario: 'ok', shot: {settingsTab: tab}}
}
const cfg = VIEWS[view] ?? VIEWS.main
const isTerminalView = view === 'terminal' || view === 'terminal-sessions' || view === 'terminal-mcp' || view === 'appearance-150' || view === 'small' || view === 'dock-150'
if (view === 'terminal-sessions') terminalShot.openSessions = true

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

// ── «Cerrar y abrir» (persist=reset|1) ─────────────────────────────────────
// Lo que Go guardaría en scadlab.db, en localStorage: settings por clave (igual que la tabla
// key/value), proyectos abiertos + activo, y el último checkpoint de cada workspace.
type Persisted = {settings: Record<string, unknown>; open: number[] | null; active: number; last: Record<number, string>}
const PERSIST_KEY = 'uishot.persist.v1'
const persistMode = q.get('persist')
if (persistMode === 'reset') localStorage.removeItem(PERSIST_KEY)
const persisted: Persisted | null = persistMode
    ? (() => {
          try {
              return JSON.parse(localStorage.getItem(PERSIST_KEY) ?? '') as Persisted
          } catch {
              return {settings: {}, open: null, active: 0, last: {}}
          }
      })()
    : null
function persist(change: (p: Persisted) => void) {
    if (!persisted) return
    change(persisted)
    localStorage.setItem(PERSIST_KEY, JSON.stringify(persisted))
}
const saveSetting = (key: string, value: unknown) => persist((p) => (p.settings[key] = value))

// Pestañas de proyecto (S8): tres proyectos abiertos y un cuarto workspace sin abrir.
const projectsShot = view.startsWith('projects-')
const sidebarLong = view === 'sidebar-long'
const withLast = <T extends {id: number}>(w: T) => ({...w, lastCheckpoint: persisted?.last[w.id] ?? ''})
const workspaces = (
    cfg.scenario === 'empty'
        ? []
        : [
              {id: 1, name: sidebarLong ? (es ? 'Caja de la Raspberry Pi 5 con un nombre de workspace realmente largo para probar el recorte' : 'Raspberry Pi 5 case with a really long workspace name to test the clipping') : es ? 'Caja Raspberry Pi 5' : 'Raspberry Pi 5 case', path: `${home}/rpi5-case`, printerId: 'bambu-a1-mini', description: '', lastOpened: 0, createdAt: 0},
              {id: 2, name: es ? 'Soporte de auriculares' : 'Headphone stand', path: `${home}/headphone-stand`, printerId: 'bambu-a1', description: '', lastOpened: 0, createdAt: 0},
              {id: 3, name: es ? 'Organizador Gridfinity' : 'Gridfinity organizer', path: `${home}/gridfinity`, printerId: 'bambu-p1s', description: '', lastOpened: 0, createdAt: 0},
              ...(projectsShot
                  ? [{id: 4, name: es ? 'Llavero personalizable' : 'Custom keychain', path: `${home}/keychain`, printerId: 'bambu-a1-mini', description: '', lastOpened: 0, createdAt: 0}]
                  : []),
          ]
).map(withLast)

// Vínculos y rótulos del árbol (S3): solo en las vistas de checkpoints avanzados,
// para que el resto de las capturas conserve el árbol de siempre.
const treeShot = ['tree-menu', 'tree-active-branch', 'trash', 'diff-parent'].includes(view)
const SESSION = 'b6f1c0de-2d3a-4c55-9e0a-7a1d5c3e8f21'
const treeExtra: Record<string, {label?: string; agentId?: string; sessionId?: string; chatId?: number}> = treeShot
    ? {
          '002-sliding-lid.scad': {label: es ? 'Tapa deslizante' : 'Sliding lid', agentId: 'claude', sessionId: SESSION},
          '003-vent-slots.scad': {agentId: 'claude', sessionId: SESSION},
          '004-usb-c-cutout.scad': {agentId: 'claude', sessionId: SESSION},
          '005-snap-lid.scad': {label: es ? 'Tapa a presión' : 'Snap-fit lid', chatId: 1},
      }
    : {}
// Padre activo explícito: en el árbol de la rama activa, el 003 (se quiere probar
// otra variante desde ahí); en el resto, ninguno (rige el más nuevo).
const activeParent = view === 'tree-active-branch' || view === 'trash' ? '003-vent-slots.scad' : ''
const cp = (n: number, file: string, parent: string, desc: string) => ({
    id: n, workspaceId: 1, fileName: file, parentFile: parent, label: '', description: desc,
    sessionId: '', agentId: '', chatId: 0, collapsed: false, fileMtime: treeShot ? 1792080000 + n * 5400 : 0, createdAt: 0,
    ...treeExtra[file],
})
// sidebar-long: una cadena de diez checkpoints con nombres de 60 caracteres, como los que deja
// un agente que itera; es lo más ancho que puede llegar a ser el árbol.
const longNames = [
    'caja-base-con-un-nombre-extremadamente-largo-de-checkpoint',
    'tapa-deslizante-con-guias-y-ranuras-de-ventilacion-laterales',
    'recorte-usb-c-y-hdmi-con-holgura-extra-para-los-cables',
    'soporte-interno-de-la-placa-con-cuatro-postes-roscados',
    'tapa-a-presion-con-lengueta-y-refuerzo-de-esquinas',
    'logo-bicolor-para-el-ams-con-texto-en-relieve-de-un-mm',
    'agujeros-de-montaje-m2-5-con-insertos-roscados-de-laton',
    'nervaduras-bajo-la-tapa-para-evitar-el-alabeo-al-imprimir',
    'filetes-de-r3-en-todas-las-aristas-exteriores-de-la-caja',
    'variante-final-lista-para-imprimir-en-la-a1-mini-de-bambu',
]
const longCheckpoints = longNames.map((n, i) =>
    cp(i + 1, `${String(i + 1).padStart(3, '0')}-${n}.scad`, i === 0 ? '' : `${String(i).padStart(3, '0')}-${longNames[i - 1]}.scad`, `${es ? 'Paso' : 'Step'} ${i + 1}: ${n.replaceAll('-', ' ')}`),
)
const checkpoints = sidebarLong ? longCheckpoints : [
    cp(1, '001-base-box.scad', '', es ? 'Caja base 92×64×32' : 'Base box 92×64×32'),
    cp(2, '002-sliding-lid.scad', '001-base-box.scad', es ? 'Tapa deslizante' : 'Sliding lid'),
    cp(3, '003-vent-slots.scad', '002-sliding-lid.scad', es ? 'Ranuras de ventilación' : 'Vent slots'),
    cp(4, '004-usb-c-cutout.scad', '003-vent-slots.scad', es ? 'Recorte USB-C y HDMI' : 'USB-C and HDMI cutouts'),
    cp(5, '005-snap-lid.scad', '002-sliding-lid.scad', es ? 'Rama: tapa a presión' : 'Branch: snap-fit lid'),
    cp(6, '006-two-color-logo.scad', '005-snap-lid.scad', es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS'),
]
// Los otros proyectos de las vistas `projects-*`: árboles cortos con otros nombres, para que
// se note a simple vista en qué proyecto se está.
const otherCheckpoints: Record<number, ReturnType<typeof cp>[]> = {
    2: [
        {...cp(21, '001-base-stand.scad', '', es ? 'Base del soporte' : 'Stand base'), workspaceId: 2},
        {...cp(22, '002-arc-support.scad', '001-base-stand.scad', es ? 'Arco de apoyo' : 'Support arc'), workspaceId: 2},
        {...cp(23, '003-cable-slot.scad', '002-arc-support.scad', es ? 'Ranura para el cable' : 'Cable slot'), workspaceId: 2},
    ],
    3: [
        {...cp(31, '001-grid-4x2.scad', '', es ? 'Cuadrícula de 4×2' : '4×2 grid'), workspaceId: 3},
        {...cp(32, '002-dividers.scad', '001-grid-4x2.scad', es ? 'Divisores' : 'Dividers'), workspaceId: 3},
    ],
}
const viewFor = (id: number) => ({
    workspace: workspaces.find((w) => w.id === id) ?? workspaces[0],
    checkpoints: id === 1 ? checkpoints : (otherCheckpoints[id] ?? []),
    missing: false,
    activeParent: id === 1 ? activeParent : '',
})
const selectedFile = sidebarLong ? checkpoints.at(-1)!.fileName : cfg.scenario === 'error' ? '004-usb-c-cutout.scad' : '006-two-color-logo.scad'

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

// Motor del render: el nativo (OpenSCAD instalado) o, en el escenario `wasm`, el integrado.
const wasmShot = cfg.scenario === 'wasm'
const engineFields = wasmShot ? {version: '2025.12.30.wasm30277', engine: 'wasm'} : {version: '2025.06.01', engine: 'native'}

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
    ...engineFields,
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
    ...engineFields,
}
const missingResult = {
    fileName: selectedFile, status: 'missing', meshPath: '', cacheHit: false, durationMs: 0,
    diagnostics: [], log: '', backend: '', version: '', engine: '',
}
const renderState: Record<string, unknown> =
    cfg.scenario === 'missing'
        ? {}
        : sidebarLong
          ? Object.fromEntries(longCheckpoints.map((c) => [c.fileName, okResult(c.fileName, 400, true)]))
          : {
              '001-base-box.scad': okResult('001-base-box.scad', 380, true),
              '002-sliding-lid.scad': okResult('002-sliding-lid.scad', 512, true),
              '003-vent-slots.scad': okResult('003-vent-slots.scad', 690, true, 1),
              '004-usb-c-cutout.scad': errorResult,
              '005-snap-lid.scad': okResult('005-snap-lid.scad', 455, true),
              '006-two-color-logo.scad': okResult('006-two-color-logo.scad', 1240, false),
          }

// Resultados de render de los otros proyectos: el 2 está bien y el 3 tiene su último render fallido.
const failedResult = (file: string) => ({
    ...errorResult,
    fileName: file,
    diagnostics: [{severity: 'error', line: 9, col: 0, file: '', message: 'Parser error: syntax error'}],
})
const stateFor = (id: number): Record<string, unknown> =>
    id === 1
        ? renderState
        : Object.fromEntries(
              (otherCheckpoints[id] ?? []).map((c, i, all) => [
                  c.fileName,
                  id === 3 && i === all.length - 1 ? failedResult(c.fileName) : okResult(c.fileName, 480 + i * 90, true),
              ]),
          )
// El código de cada proyecto empieza distinto, para reconocerlo al volver a su pestaña.
const sourceFor = (id: number, file: string) =>
    id === 1
        ? file === '004-usb-c-cutout.scad'
            ? brokenSource
            : source
        : `// ${workspaces.find((w) => w.id === id)?.name ?? ''} · ${file}\nwidth = ${40 + id * 7};   // [20:1:120]\ncube([width, width / 2, 10]);\n`

// Lo que devolvería LintCheckpointSource (backend/scad) sobre cada código:
// las líneas coinciden con los fuentes de arriba.
const diag = (severity: string, line: number, col: number, endCol: number, code: string, msg: [string, string]) => ({
    severity, line, col, endLine: line, endCol, code, source: code === 'syntax' ? 'openscad' : 'scadlab', message: es ? msg[0] : msg[1],
})
const lintFor = (file: string) =>
    file === '004-usb-c-cutout.scad'
        ? [
              diag('warning', 2, 9, 30, 'SL005', [
                  'un checkpoint no debe incluir a otro (003-vent-slots.scad): cada checkpoint es autocontenido; copia lo que necesites',
                  'a checkpoint must not include another one (003-vent-slots.scad): each checkpoint is self-contained; copy what you need',
              ]),
              diag('info', 7, 1, 7, 'SL009', [
                  'parámetro «port_z» del Customizer sin rango: agrega // [min:paso:max] al final de la línea',
                  "Customizer parameter 'port_z' has no range: add // [min:step:max] at the end of the line",
              ]),
              diag('error', 23, 1, 0, 'syntax', ['Parser error: syntax error', 'Parser error: syntax error']),
          ]
        : [
              diag('info', 7, 1, 4, 'SL009', [
                  'parámetro «lip» del Customizer sin rango: agrega // [min:paso:max] al final de la línea',
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

// Diseños propios inventados (los que lista «Diseños guardados…»). Cada uno es el
// sobre de un diseño real: se arma al vuelo con el predeterminado como base.
const savedLayouts =
    view === 'dock-layouts'
        ? [
              {name: es ? 'Revisión con agente' : 'Review with agent', json: '{"v":1,"dock":{"grid":{"root":{"type":"branch","data":[]},"height":1,"width":1,"orientation":"HORIZONTAL"},"panels":{"viewport":{"id":"viewport","contentComponent":"slot"},"source":{"id":"source","contentComponent":"slot"},"agent-1":{"id":"agent-1","contentComponent":"slot"},"chat":{"id":"chat","contentComponent":"slot"}}}}'},
              {name: es ? 'Solo código y chat' : 'Code and chat only', json: '{"v":1,"dock":{"grid":{"root":{"type":"branch","data":[]},"height":1,"width":1,"orientation":"HORIZONTAL"},"panels":{"source":{"id":"source","contentComponent":"slot"},"chat":{"id":"chat","contentComponent":"slot"}}}}'},
              {name: es ? 'Presentación' : 'Presentation', json: '{"v":1,"dock":{"grid":{"root":{"type":"branch","data":[]},"height":1,"width":1,"orientation":"HORIZONTAL"},"panels":{"viewport":{"id":"viewport","contentComponent":"slot"}}}}'},
          ]
        : []

const settings = {
    language: lang, theme, openscadPath: '', renderBackend: 'manifold', workspacesRoot: '',
    defaultPrinter: 'bambu-a1-mini', defaultMaterial: 'pla', infillPercent: 15, defaultAgent: 'claude',
    wallLoops: 2, lineWidthMm: 0.42, filamentDiameterMm: 1.75, currency: 'USD',
    autoRender: true, renderTimeoutSec: 300, autoIterate: true, bambuStudioPath: '',
    editorIndent: 4, editorFontSize: 13, editorWordWrap: false, editorMinimap: false,
    uiScale: scale, terminalFontSize: 13, chatFontSize: 13,
    layout: {sidebarWidth: 264, sidebarCollapsed: view === 'dock-hidden', codeWidth: 440, bottomHeight: 220, dimsCollapsed: view === 'viewer-split', statusBarHidden: false, workspacesHeight: 150},
    // Sin diseño guardado: el predeterminado. Los diseños con nombre son datos inventados.
    dockLayout: q.get('dockraw') ?? '',
    dockLayouts: savedLayouts,
    swatches: ['#ff7a1a', '#e7e9ee', '#2b2f38', '#3ccf7a', '#5aa2ff', '#d32f2f'],
    viewer: {wireframe: false, edges: false, ortho: [false, false], split: false, colorIndex: 0, modelColors: true},
}
// «Abrir otra vez»: lo que se guardó en la sesión anterior pisa los valores de fábrica del banco.
if (persisted) Object.assign(settings, persisted.settings)

// Mallas de ejemplo (frontend/public/uishot/). La caja es enclosure.stl, de la plantilla
// electronics-enclosure.scad; sus medidas, calculadas sobre ese STL, son las que
// devolvería GetMeshInfo. Los modelos BICOLOR de S7 salen de la plantilla real
// nameplate-two-color.scad, exportada UNA vez con OpenSCAD, una malla por color
// (PART=all|base|text) y pasada a STL binario; las medidas de cada parte se
// calcularon sobre esos archivos. Para regenerarlas:
//   T=assets/skills/openscad-templates/templates/nameplate-two-color.scad
//   for p in all base text; do openscad -o nameplate-$p.stl --backend=manifold \
//     -D "PART=\"$p\"" -D '$fs=0.6' -D '$fa=10' $T; done
//   (y la ancha, que no cabe en una A1 mini: -D 'label="Taller 3D ScadLab"'
//    -D plate_w=215 -D plate_d=40 -D 'PLATE=[400,400,400]', como nameplate-wide-*)
const sampleInfo = {
    triangles: 4176, min: [0, 0, 0], max: [78, 124, 27.6], size: [78, 124, 27.6],
    volumeMm3: 30705.3, areaMm2: 35148, watertight: true,
}
type ShotPart = {name: string; color: string; uncolored: boolean; file: string; info: typeof sampleInfo}
type ShotModel = {mesh: string; info: typeof sampleInfo; parts: ShotPart[]}
const plateModel = (wide: boolean): ShotModel => {
    const pre = wide ? 'nameplate-wide' : 'nameplate'
    return {
        mesh: `/uishot/${pre}-all.stl`,
        info: wide ? NAMEPLATE_WIDE.all : NAMEPLATE.all,
        parts: [
            {name: 'Color 1 (#FFFFFF)', color: '#FFFFFF', uncolored: false, file: `/uishot/${pre}-base.stl`, info: wide ? NAMEPLATE_WIDE.base : NAMEPLATE.base},
            {name: 'Color 2 (#D32F2F)', color: '#D32F2F', uncolored: false, file: `/uishot/${pre}-text.stl`, info: wide ? NAMEPLATE_WIDE.text : NAMEPLATE.text},
        ],
    }
}
const NAMEPLATE = {
    all: {triangles: 3318, min: [-55.0, -16.0, 0.0], max: [55.0, 16.0, 3.6], size: [110.0, 32.0, 3.6], volumeMm3: 10951.56, areaMm2: 8309.7, watertight: true},
    base: {triangles: 236, min: [-55.0, -16.0, 0.0], max: [55.0, 16.0, 3.0], size: [110.0, 32.0, 3.0], volumeMm3: 10495.94, areaMm2: 7778.2, watertight: true},
    text: {triangles: 3068, min: [-53.0, -14.0, 3.0], max: [53.0, 14.0, 3.6], size: [106.0, 28.0, 0.6], volumeMm3: 455.62, areaMm2: 2050.2, watertight: true},
}
const NAMEPLATE_WIDE = {
    all: {triangles: 5244, min: [-107.5, -20.0, 0.0], max: [107.5, 20.0, 3.6], size: [215.0, 40.0, 3.6], volumeMm3: 26588.95, areaMm2: 19575.9, watertight: true},
    base: {triangles: 236, min: [-107.5, -20.0, 0.0], max: [107.5, 20.0, 3.0], size: [215.0, 40.0, 3.0], volumeMm3: 25717.86, areaMm2: 18563.2, watertight: true},
    text: {triangles: 4972, min: [-105.5, -18.0, 3.0], max: [105.5, 18.0, 3.6], size: [211.0, 36.0, 0.6], volumeMm3: 871.09, areaMm2: 3916.3, watertight: true},
}
// Un modelo sin color() es UNA parte amarilla "sin color": el visor la pinta con la muestra.
const enclosureModel: ShotModel = {
    mesh: '/uishot/enclosure.stl',
    info: sampleInfo,
    parts: [{name: 'Color 1 (#F9D72C)', color: '#F9D72C', uncolored: true, file: '/uishot/enclosure.stl', info: sampleInfo}],
}
const shotModel: ShotModel = view === 'plate-overflow' ? plateModel(true) : view === 'viewer-parts' || view === 'print-panel' || view === 'persist' || view.startsWith('projects-') ? plateModel(false) : enclosureModel
const partFiles = shotModel.parts.map((p) => p.file)
const realFetch = window.fetch.bind(window)
window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url
    if (url.startsWith('/mesh/')) return realFetch(shotModel.mesh, init)
    const part = /^\/parts\/[0-9a-f]{64}\/(\d\d)\.stl$/.exec(url)
    if (part) return realFetch(partFiles[Number(part[1])] ?? shotModel.mesh, init)
    return realFetch(input, init)
}

// ── Terminal de agentes (bloque 5.A) ────────────────────────────────────────
// Salida inventada de una sesión de Claude Code, con colores ANSI: es lo que
// el banco le entrega a xterm.js como «salida reciente» de la terminal abierta.
const ESC = '\x1b['
const c = (code: string, text: string) => `${ESC}${code}m${text}${ESC}0m`
const agentScreen = [
    c('1;38;5;208', '✻ Claude Code') + c('90', es ? '  ·  Sonnet 5.5  ·  rpi5-case' : '  ·  Sonnet 5.5  ·  rpi5-case'),
    '',
    c('1', '> ') + (es ? 'Agrega cuatro tornillos M2.5 a la tapa, en naranja, a 6 mm de las esquinas.' : 'Add four M2.5 screws to the lid, in orange, 6 mm from the corners.'),
    '',
    c('38;5;250', '● ') + (es ? 'Leo el último checkpoint y sigo las reglas del workspace.' : 'I will read the latest checkpoint and follow the workspace rules.'),
    '',
    c('38;5;250', '● ') + c('1;34', 'Read') + c('90', '(006-two-color-logo.scad)'),
    c('90', es ? '  ⎿  41 líneas leídas' : '  ⎿  Read 41 lines'),
    '',
    c('38;5;250', '● ') + (es
        ? 'El modelo ya declara `lip` y `wall`; los tornillos usan las mismas.'
        : 'The model already declares `lip` and `wall`; the screws will reuse them.'),
    '',
    c('38;5;250', '● ') + c('1;32', 'Write') + c('90', '(007-lid-screws.scad)'),
    c('90', es ? '  ⎿  44 líneas escritas' : '  ⎿  Wrote 44 lines'),
    c('32', '     + // @checkpoint parent=006-two-color-logo.scad desc="' + (es ? 'Tornillos de la tapa' : 'Lid screws') + '"'),
    c('32', '     + screw_d = 2.5;   // [2:0.1:4]'),
    c('32', '     + inset   = 6;     // [4:0.5:12]'),
    c('31', '     - for (p = [[6, 6], [width - 6, depth - 6]])'),
    c('32', '     + for (p = [[inset, inset], [width - inset, inset],'),
    c('32', '     +           [inset, depth - inset], [width - inset, depth - inset]])'),
    c('32', '     +     color("#ff7a1a") translate([p.x, p.y, height]) cylinder(d = screw_d, h = 4);'),
    '',
    c('38;5;250', '● ') + (es ? 'Listo: ' : 'Done: ') + c('1', '007-lid-screws.scad') + (es ? ', hija de 006. ScadLab lo renderiza solo.' : ', child of 006. ScadLab renders it on its own.'),
    '',
    c('90', '╭' + '─'.repeat(70) + '╮'),
    c('90', '│ ') + c('1', '>') + ' '.repeat(68) + c('90', '│'),
    c('90', '╰' + '─'.repeat(70) + '╯'),
    c('90', es ? '  ? para atajos' : '  ? for shortcuts'),
].join('\r\n') + '\r\n' + ESC + '3A' + ESC + '4G' // el cursor queda dentro de la caja de entrada
// Misma sesión, pero el agente trabaja con las herramientas del servidor MCP de
// ScadLab (render, medir, ver la pieza) en lugar de invocar OpenSCAD a mano.
const agentScreenMcp = [
    c('1;38;5;208', '✻ Claude Code') + c('90', '  ·  Sonnet 5.5  ·  rpi5-case'),
    '',
    c('1', '> ') + (es ? 'Revisa que la tapa de tornillos entre en la placa y muéstrame cómo queda.' : 'Check that the screw lid fits the plate and show me how it looks.'),
    '',
    c('38;5;250', '● ') + c('1;35', 'scadlab') + c('90', ' - render ') + c('90', '(MCP)') + c('90', '(file: "007-lid-screws.scad")'),
    c('90', es ? '  ⎿  ok · 0,4 s · 92 × 64 × 36 mm · 41 280 mm³ · estanca' : '  ⎿  ok · 0.4 s · 92 × 64 × 36 mm · 41,280 mm³ · watertight'),
    '',
    c('38;5;250', '● ') + c('1;35', 'scadlab') + c('90', ' - measure ') + c('90', '(MCP)') + c('90', '(file: "007-lid-screws.scad")'),
    c('90', es ? '  ⎿  cabe en la placa de la A1 mini (180 × 180 × 180) · ≈ 51 g en PLA' : '  ⎿  fits the A1 mini plate (180 × 180 × 180) · ≈ 51 g in PLA'),
    '',
    c('38;5;250', '● ') + c('1;35', 'scadlab') + c('90', ' - preview ') + c('90', '(MCP)') + c('90', '(file: "007-lid-screws.scad", view: "iso")'),
    c('90', es ? '  ⎿  [imagen PNG · 800 × 600]' : '  ⎿  [PNG image · 800 × 600]'),
    '',
    c('38;5;250', '● ') + (es
        ? 'La tapa mide 92 × 64 × 36 mm y cabe de sobra. Los cuatro tornillos naranjas quedan a 6 mm de las esquinas, como pediste.'
        : 'The lid measures 92 × 64 × 36 mm and fits with room to spare. The four orange screws sit 6 mm from the corners, as you asked.'),
    '',
    c('90', '╭' + '─'.repeat(70) + '╮'),
    c('90', '│ ') + c('1', '>') + ' '.repeat(68) + c('90', '│'),
    c('90', '╰' + '─'.repeat(70) + '╯'),
    c('90', es ? '  ? para atajos' : '  ? for shortcuts'),
].join('\r\n') + '\r\n' + ESC + '3A' + ESC + '4G'
const toB64 = (text: string) => {
    let bin = ''
    for (const b of new TextEncoder().encode(text)) bin += String.fromCharCode(b)
    return btoa(bin)
}
const sessionAt = (d: number, h: number, m: number) => new Date(2026, 9, d, h, m).getTime()
const claudeSessions = [
    {id: '5b0f2c1e-3a47-4d6e-9c18-0a1e7f3b2d11', title: es ? 'Tornillos M2.5 de la tapa' : 'M2.5 lid screws', updatedAt: sessionAt(15, 13, 20)},
    {id: '7c9d4e2a-1b58-4f0a-8e3d-2b6a9c1f4e22', title: es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS', updatedAt: sessionAt(15, 9, 5)},
    {id: '9e1a6b3c-2d69-4a1b-9f4e-3c7bad2a5f33', title: es ? 'Recorte USB-C y HDMI' : 'USB-C and HDMI cutouts', updatedAt: sessionAt(14, 17, 42)},
    {id: 'a2b7c8d4-3e7a-4b2c-8a5f-4d8cbe3b6a44', title: es ? 'Ranuras de ventilación laterales' : 'Side vent slots', updatedAt: sessionAt(13, 11, 30)},
    {id: 'b3c8d9e5-4f8b-4c3d-9b6a-5e9dcf4c7b55', title: es ? 'Caja Raspberry Pi 5 con tapa deslizante' : 'Raspberry Pi 5 case with sliding lid', updatedAt: sessionAt(9, 16, 15)},
]

// ── Chat con modelos (S5, bloque 5.B) ──────────────────────────────────────
// Tres chats inventados; el más reciente trae una respuesta con un bloque
// OpenSCAD (ese es el que muestra «Copiar» y «Guardar como checkpoint»).
const chatItems = [
    {id: 3, workspaceId: 1, providerId: 'ollama', model: 'qwen3-coder:30b', title: es ? 'Caja para Raspberry Pi 5 con tapa deslizante' : 'Raspberry Pi 5 case with a sliding lid', createdAt: 1_790_000_000, updatedAt: 1_790_003_000},
    {id: 2, workspaceId: 1, providerId: 'ollama', model: 'llama3.2:3b', title: es ? 'Ranuras de ventilación' : 'Ventilation slots', createdAt: 1_789_900_000, updatedAt: 1_789_900_600},
    {id: 1, workspaceId: 1, providerId: 'ollama', model: 'qwen3-coder:30b', title: es ? 'Cuántos milímetros de holgura para una tapa' : 'How much clearance for a lid', createdAt: 1_789_800_000, updatedAt: 1_789_800_400},
]
const chatScad = `// @checkpoint parent=006-two-color-logo.scad desc="${es ? 'Caja para Raspberry Pi 5 con tapa deslizante' : 'Raspberry Pi 5 case with a sliding lid'}"
board_w = 85;   // [60:1:120]
board_d = 56;   // [40:1:90]
wall    = 2;    // [1.2:0.2:4]
height  = 24;   // [16:1:48]

color("#2b2f38") difference() {
    cube([board_w + 2*wall, board_d + 2*wall, height]);
    translate([wall, wall, wall]) cube([board_w, board_d, height]);
}
`
const chatMessages = [
    {
        id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900,
        content: es ? 'Haz una caja para una Raspberry Pi 5 con tapa deslizante.' : 'Make a box for a Raspberry Pi 5 with a sliding lid.',
    },
    {
        id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000,
        content: es
            ? `Te dejo la caja de la **Raspberry Pi 5**, con paredes de 2 mm que imprimen bien en la A1 mini:\n\n\`\`\`openscad\n${chatScad}\`\`\`\n\n- La tapa conviene en un checkpoint aparte, para imprimirla plana.\n- Dime si quieres ranuras de ventilación o recortes para los puertos.`
            : `Here is the **Raspberry Pi 5** case, with 2 mm walls that print well on the A1 mini:\n\n\`\`\`openscad\n${chatScad}\`\`\`\n\n- The lid is better as its own checkpoint, so it prints flat.\n- Tell me if you want ventilation slots or port cutouts.`,
    },
]
// En `chat-stream` la respuesta sigue en curso al abrir el panel: GetChat trae
// lo escrito hasta el trozo 2 y después llegan eventos (el 2 repetido, que
// hay que descartar, y el 3 y el 4), como pasa al reabrir la pestaña a mitad.
const streamParts = es
    ? ['Sumo las ranuras de ventilación en la pared trasera:\n\n```openscad\n', 'slots = 6;\nfor (i = [0 : slots - 1])\n', '    translate([8 + i * 10, board_d, 6])\n', '        cube([6, wall + 1, 14]);\n']
    : ['Adding ventilation slots on the back wall:\n\n```openscad\n', 'slots = 6;\nfor (i = [0 : slots - 1])\n', '    translate([8 + i * 10, board_d, 6])\n', '        cube([6, wall + 1, 14]);\n']
const chatMocks = {
    ListChats: () => P(view === 'chat-new' ? [] : chatItems),
    GetChat: (id: number) => {
        if (view === 'chat-stream') {
            setTimeout(() => {
                emit('chat:delta', {chatId: id, text: streamParts[1], seq: 2})
                emit('chat:delta', {chatId: id, text: streamParts[2], seq: 3})
                emit('chat:delta', {chatId: id, text: streamParts[3], seq: 4})
            }, 200)
            return P({
                chat: chatItems[0],
                messages: [chatMessages[0], {id: 3, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: chatMessages[1].content}, {id: 4, chatId: 3, role: 'user', createdAt: 1_790_003_100, content: es ? 'Agrega ranuras de ventilación atrás.' : 'Add ventilation slots at the back.'}],
                running: true, partial: streamParts[0] + streamParts[1], partialSeq: 2,
            })
        }
        return P({
            chat: chatItems.find((c) => c.id === id) ?? chatItems[0],
            messages: id === 3 ? chatMessages : [],
            running: false, partial: '', partialSeq: 0,
        })
    },
    ListProviderModels: () => P(['qwen3-coder:30b', 'llama3.2:3b', 'gemma3:12b']),
    ChatCheckpointParent: () => P(selectedFile),
    NewChat: () => P(chatItems[0]),
    SendChatMessage: () => P(chatMessages[0]),
    CancelChat: () => P(undefined),
    DeleteChat: () => P(undefined),
}

// Exportación 3MF: lo que devolvería PreviewExportParts. Los nombres tienen la
// forma real del backend («Color N (#RRGGBB)»).
const part = (n: number, hex: string, triangles: number) => ({name: `Color ${n} (${hex})`, color: hex, triangles, extruder: n, uncolored: false})
const exportParts =
    view === 'export-3mf-warning'
        ? [part(1, '#2B2F38', 3412), part(2, '#FF7A1A', 764), part(3, '#E7E9EE', 1180), part(4, '#3CCF7A', 522), part(5, '#5AA2FF', 396)]
        : [part(1, '#2B2F38', 3412), part(2, '#FF7A1A', 764)]
const exportWarnings =
    view === 'export-3mf-warning'
        ? [
              es
                  ? 'El modelo usa 5 colores y tu Bambu Lab A1 mini admite 4 a la vez (AMS lite). Unifica algunos colores o imprime por partes.'
                  : 'The model uses 5 colors and your Bambu Lab A1 mini supports 4 at a time (AMS lite). Merge some colors or print in separate parts.',
          ]
        : []
const bambuApp = platform === 'windows' ? 'C:\\Program Files\\Bambu Studio\\bambu-studio.exe' : '/Applications/BambuStudio.app'

// Estado del servidor MCP: el ejecutable de ScadLab y los archivos del primer
// workspace, todos al día (lo que ve el usuario en el caso normal).
const scadlabBin = platform === 'windows' ? 'C:\\Program Files\\ScadLab\\ScadLab.exe' : '/Applications/ScadLab.app/Contents/MacOS/ScadLab'
const mcpDir = `${home}${platform === 'windows' ? '\\' : '/'}rpi5-case`
const mcpFiles = [
    {agent: 'claude', name: '.mcp.json', path: `${mcpDir}/.mcp.json`, status: 'ok'},
    {agent: 'opencode', name: 'opencode.json', path: `${mcpDir}/opencode.json`, status: 'ok'},
    {agent: 'gemini', name: '.gemini/settings.json', path: `${mcpDir}/.gemini/settings.json`, status: 'ok'},
]
const quoteIfSpaced = (p: string) => (p.includes(' ') ? `"${p}"` : p)
const mcpStatus = (id: number) => ({
    enabled: true,
    binary: scadlabBin,
    binaryError: '',
    workspaceDir: id ? mcpDir : '',
    command: `${quoteIfSpaced(scadlabBin)} --mcp --workspace ${quoteIfSpaced(mcpDir)}`,
    files: id ? mcpFiles : [],
    codexFlags: [],
    openscad: true,
    openscadPath: '/Applications/OpenSCAD.app/Contents/MacOS/OpenSCAD',
})

const materialList = [
    {id: 'pla', name: 'PLA', densityGCm3: 1.24, costPerKg: 20},
    {id: 'petg', name: 'PETG', densityGCm3: 1.27, costPerKg: 22},
    {id: 'abs', name: 'ABS', densityGCm3: 1.04, costPerKg: 22},
    {id: 'asa', name: 'ASA', densityGCm3: 1.07, costPerKg: 25},
    {id: 'tpu', name: 'TPU', densityGCm3: 1.21, costPerKg: 35},
]

// Papelera inventada de la vista `trash`: una rama de variantes descartada.
const trashItems = [
    {name: '20261015-101500-008-wall-fillets.scad', original: '008-wall-fillets.scad', parent: '006-two-color-logo.scad', description: es ? 'Filetes en las paredes (se rompió la tapa)' : 'Wall fillets (broke the lid)', trashedAt: 1792059300},
    {name: '20261015-101500-007-thin-walls.scad', original: '007-thin-walls.scad', parent: '006-two-color-logo.scad', description: es ? 'Paredes de 1.2 mm para ahorrar filamento' : '1.2 mm walls to save filament', trashedAt: 1792059300},
    {name: '20261014-184000-004-extra-ribs.scad', original: '004-extra-ribs.scad', parent: '003-vent-slots.scad', description: es ? 'Nervaduras extra bajo la tapa' : 'Extra ribs under the lid', trashedAt: 1791999600},
]

// El padre del checkpoint abierto en la vista `diff-parent`: el mismo código sin
// el logo y con otra anchura, para que el diff tenga cambios reales.
const parentSource = () =>
    source
        .replace('parent=005-snap-lid.scad', 'parent=002-sliding-lid.scad')
        .replace(es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS', es ? 'Tapa a presión' : 'Snap-fit lid')
        .replace('width  = 92;', 'width  = 88;')
        .replace(/color\("#ff7a1a"\)\n[^\n]*\n[^\n]*\n\n/, '')

const agyStatus = {state: 'missing', agyPath: '/Users/maker/.local/bin/agy', command: `${scadlabBin} --mcp`, current: '', detail: ''}

const App_ = {
    // Bambu Studio detectado; abrir y mostrar no hacen nada (el banco no lanza apps).
    DetectSlicers: () => P([{id: 'bambu-studio', name: 'Bambu Studio', path: bambuApp, found: true}]),
    OpenInSlicer: () => P(undefined),
    RevealPath: () => P(undefined),
    PreviewExportParts: () => P({parts: exportParts, warnings: exportWarnings}),
    ExportCheckpoint: () => P({path: '', format: '3mf', parts: exportParts, warnings: exportWarnings}),
    GetBootstrap: () => P({version: '0.1.0', platform, dataDir: platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\ScadLab' : '/Users/maker/Library/Application Support/ScadLab', schemaVersion: 1, initError: '', settings}),
    GetSettings: () => P(settings),
    ListPrinters: () => P(printers),
    ListWorkspaces: () => P(workspaces),
    OpenWorkspace: (id: number) => P(viewFor(id)),
    RescanWorkspace: (id: number) => P(viewFor(id)),
    ReadCheckpoint: (id: number, file: string) => P(sourceFor(id, file)),
    // Proyectos abiertos (S8): en las vistas `projects-*` hay tres pestañas (la activa es la
    // primera); en las demás, una sola, como siempre.
    ListOpenProjects: () => {
        const ids = persisted?.open ?? (cfg.scenario === 'empty' ? [] : projectsShot ? [1, 2, 3] : [1])
        return P({
            projects: ids.map((id) => ({workspace: workspaces.find((w) => w.id === id)!, missing: false})),
            activeId: persisted?.open ? persisted.active : cfg.scenario === 'empty' ? 0 : 1,
        })
    },
    OpenProject: (id: number) => {
        persist((p) => {
            const open = p.open ?? (projectsShot ? [1, 2, 3] : [1])
            p.open = open.includes(id) ? open : [...open, id]
            p.active = id
        })
        return P(viewFor(id))
    },
    CloseProject: (id: number) => {
        persist((p) => {
            p.open = (p.open ?? []).filter((x) => x !== id)
            if (p.active === id) p.active = p.open[0] ?? 0
        })
        return P(undefined)
    },
    SetActiveProject: (id: number) => {
        persist((p) => (p.active = id))
        return P(undefined)
    },
    ReorderProjects: (ids: number[]) => {
        persist((p) => (p.open = ids))
        return P(undefined)
    },
    // Binding nuevo (migración 4): el checkpoint elegido de cada proyecto.
    SetLastCheckpoint: (id: number, file: string) => {
        persist((p) => (p.last[id] = file))
        return P(undefined)
    },
    SaveViewer: (v: unknown) => {
        saveSetting('viewer', v)
        return P(v)
    },
    // «Nuevo workspace»: Go calcula la ruta con workspace.Slug; aquí la misma cuenta (lib/slug.ts
    // inventa 'model' para un nombre vacío, y Go devuelve ""), y la misma regla de ruta absoluta.
    PreviewWorkspacePath: (name: string, parent: string) => {
        const base = parent.trim() || home
        const sep = platform === 'windows' ? '\\' : '/'
        const abs = /^([a-zA-Z]:[\\/]|\\\\|\/)/.test(base)
        if (!abs || !/[a-z0-9]/i.test(name.normalize('NFD'))) return P('')
        return P(base.replace(/[\\/]+$/, '') + sep + slugify(name))
    },
    CreateWorkspaceIn: (name: string, _printer: string, parent: string) => {
        const base = parent.trim() || home
        return P({...workspaces[0], id: 99, name, path: `${base}/${slugify(name)}`, lastCheckpoint: ''})
    },
    SetWorkspacesRoot: (dir: string) => {
        saveSetting('workspacesRoot', dir)
        return P(undefined)
    },
    // Uno con render en curso (2) y otro con su último render fallido y un agente vivo (3).
    ProjectActivity: () =>
        P(
            projectsShot
                ? [
                      {id: 1, rendering: false, latestFile: '006-two-color-logo.scad', lastRenderStatus: 'ok', renderFailed: false, agentsAlive: 0},
                      {id: 2, rendering: true, latestFile: '003-cable-slot.scad', lastRenderStatus: 'ok', renderFailed: false, agentsAlive: 0},
                      {id: 3, rendering: false, latestFile: '002-dividers.scad', lastRenderStatus: 'error', renderFailed: true, agentsAlive: 1},
                  ]
                : [],
        ),
    EffectiveWorkspacesRoot: () => P(home),
    GetRenderState: (id: number) => P(stateFor(id)),
    // Igual que el backend: emite render:started / render:finished, que es lo
    // que mira la UI (el valor devuelto solo lo usa quien llama).
    RenderCheckpoint: (id: number, file: string) => {
        const result = cfg.scenario === 'missing' ? missingResult : (renderState[file] ?? okResult(file, 600, false))
        emit('render:started', {workspaceId: id, fileName: file})
        setTimeout(() => emit('render:finished', {workspaceId: id, result}), 50)
        return P(result)
    },
    CancelRender: () => P(undefined),
    GetMeshInfo: () => P(shotModel.info),
    // Partes por color de S7 (lo que devolvería backend/parts) y su estimación,
    // con la misma fórmula que backend/estimate.
    GetModelParts: (_id: number, file: string) => {
        const key = fakeKey(file)
        const plate = view === 'plate-overflow' ? {x: 180, y: 180, z: 180} : {x: 180, y: 180, z: 180}
        const size = shotModel.info.size
        const over = (v: number, max: number) => (v > max + 0.05 ? v - max : 0)
        const overflow = {x: over(size[0], plate.x), y: over(size[1], plate.y), z: over(size[2], plate.z)}
        return P({
            key,
            parts: shotModel.parts.map((p, i) => ({index: i, name: p.name, color: p.color, uncolored: p.uncolored, url: `/parts/${key}/${String(i).padStart(2, '0')}.stl`, info: p.info})),
            total: shotModel.info,
            plate,
            fits: overflow.x + overflow.y + overflow.z === 0,
            overflow,
        })
    },
    EstimatePrint: (_id: number, _file: string, o: {infillPercent: number; wallLoops: number; lineWidthMm: number; filamentDiameterMm: number; defaultMaterialId: string; partMaterials: Record<number, string>; currency: string}) => {
        const mats = materialList
        const wall = o.wallLoops * o.lineWidthMm
        const area = Math.PI * (o.filamentDiameterMm / 2) ** 2
        const parts = shotModel.parts.map((p, i) => {
            const id = o.partMaterials?.[i] ?? o.defaultMaterialId
            const m = mats.find((x) => x.id === id) ?? mats[0]
            const shell = Math.min(p.info.volumeMm3, p.info.areaMm2 * wall)
            const effectiveMm3 = shell + (p.info.volumeMm3 - shell) * (o.infillPercent / 100)
            const massG = (effectiveMm3 / 1000) * m.densityGCm3
            return {index: i, materialId: m.id, volumeMm3: p.info.volumeMm3, effectiveMm3, massG, lengthM: effectiveMm3 / area / 1000, cost: (massG / 1000) * m.costPerKg}
        })
        const sum = (f: (p: (typeof parts)[number]) => number) => parts.reduce((n, p) => n + f(p), 0)
        return P({
            parts,
            total: {massG: sum((p) => p.massG), lengthM: sum((p) => p.lengthM), cost: sum((p) => p.cost)},
            currency: o.currency,
            assumptions: es
                ? ['Sin soportes, balsa ni borde.', 'Sin la purga del AMS al cambiar de color.', 'El relleno se cuenta como un porcentaje del interior, sin mirar su forma.']
                : ['No supports, raft or brim.', 'No AMS purge when changing color.', 'Infill is counted as a percentage of the interior, whatever its pattern.'],
        })
    },
    SaveSwatches: () => P(undefined),
    SaveScreenshot: () => P(''),
    SaveLayout: (l: unknown) => {
        saveSetting('layout', l)
        return P(l)
    },
    // Lo último que se guardó, para que las pruebas con CDP lo lean.
    SaveDockLayout: (j: string) => {
        ;(window as unknown as {__dockSaved?: string}).__dockSaved = j
        saveSetting('dockLayout', j)
        return P(undefined)
    },
    SaveDockLayouts: (list: unknown) => {
        saveSetting('dockLayouts', list)
        return P(list)
    },
    SaveAppearance: (a: unknown) => {
        for (const [k, v] of Object.entries(a as Record<string, unknown>)) saveSetting(k, v)
        return P(a)
    },
    LintCheckpointSource: (_id: number, file: string) => P(lintFor(file)),
    FormatScad: (src: string) => P(src),
    SaveAsCheckpoint: () => P('007-lid-screws.scad'),
    SaveChatCheckpoint: () => P('007-lid-screws.scad'),
    // Checkpoints avanzados (S3): la papelera de la vista `trash` tiene tres
    // checkpoints de una rama que se descartó; en las demás está vacía.
    ListTrash: () => P(view === 'trash' ? trashItems : []),
    TrashCheckpoint: () => P(['20261015-120000-001-base-box.scad']),
    RestoreCheckpoint: (_id: number, name: string) => P(name.replace(/^\d{8}-\d{6}-/, '')),
    SetActiveParent: () => P(undefined),
    GetActiveParent: () => P(activeParent),
    RenameCheckpoint: () => P(undefined),
    RevealCheckpoint: () => P(undefined),
    ResumeCheckpointSession: () => P({kind: 'terminal', terminalId: 'term-1', agentId: 'claude', chatId: 0}),
    ReadCheckpointPair: (_id: number, file: string) => P({file, parent: '005-snap-lid.scad', parentSource: parentSource(), source}),
    ...chatMocks,
    GetScadCatalog: () => P(catalog),
    // Terminal de agentes: en las vistas `terminal*` hay un Claude Code abierto.
    ListTerminals: () =>
        P([
            ...(isTerminalView ? [{id: 'term-1', kind: 'agent', agentId: 'claude', workspaceId: 1, cols: 100, rows: 28, startedAt: 1}] : []),
            // Un agente trabajando en el proyecto 3 (de fondo) en las vistas de pestañas.
            ...(projectsShot ? [{id: 'term-3', kind: 'agent', agentId: 'claude', workspaceId: 3, cols: 100, rows: 28, startedAt: 2}] : []),
        ]),
    ReadTerminalBuffer: () => {
        const screen = view === 'terminal-mcp' ? agentScreenMcp : agentScreen
        return P({data: toB64(screen), seq: screen.length})
    },
    ResizeTerminal: () => P(undefined),
    WriteTerminal: () => P(undefined),
    CloseTerminal: () => P(undefined),
    SetAutoIterate: () => P(undefined),
    StartAgentTerminal: () => P('term-1'),
    ListAgentChoices: () =>
        P([
            {id: 'claude', name: 'Claude Code', found: true, path: '/Users/maker/.local/bin/claude', enabled: true, canList: true},
            {id: 'opencode', name: 'opencode', found: true, path: '/Users/maker/.opencode/bin/opencode', enabled: true, canList: true},
            {id: 'codex', name: 'Codex', found: true, path: '/Users/maker/.local/bin/codex', enabled: true, canList: true},
            {id: 'antigravity', name: 'Antigravity', found: false, path: '', enabled: true, canList: false},
        ]),
    ListAgentSessions: () => P(claudeSessions),
    // Servidor MCP (S6): los tres archivos del workspace al día y Codex por flags.
    GetMCPStatus: (id: number) => P(mcpStatus(id)),
    SetMCPEnabled: () => P(undefined),
    SyncWorkspaceMCP: () => P({files: mcpFiles}),
    // Antigravity (global): agy instalado y sin la entrada, para que se vean los dos botones.
    AntigravityMCPStatus: () => P(agyStatus),
    AddAntigravityMCP: () => P(agyStatus),
    RemoveAntigravityMCP: () => P(agyStatus),
    // «Descargar OpenSCAD» (S11): sin nada descargado, con la descarga en curso en la vista de Configuración.
    GetOpenSCADDownload: () =>
        P({
            version: '2025.12.30', sizeBytes: 62687889, supported: true, installed: false, installedPath: '',
            running: view === 'settings-openscad-download', wasmVersion: '2025.12.30.wasm30277',
        }),
    DownloadOpenSCAD: () => new Promise(() => {}),
    CancelOpenSCADDownload: () => P(undefined),
    DetectTools: () =>
        P([
            wasmShot
                ? {id: 'openscad', found: false, path: '', version: '', source: ''}
                : {id: 'openscad', found: true, path: '/Applications/OpenSCAD.app/Contents/MacOS/OpenSCAD', version: '2025.06.01', source: 'system'},
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
            {id: 'ollama', kind: 'ollama', name: 'Ollama', command: '', config: {baseUrl: 'http://localhost:11434', model: 'qwen3-coder:30b'}, enabled: view !== 'chat-empty', sortOrder: 5},
            {id: 'openai', kind: 'openai', name: 'OpenAI-compatible', command: '', config: {baseUrl: 'http://localhost:1234/v1', model: '', apiKeyEnv: 'OPENAI_API_KEY'}, enabled: false, sortOrder: 6},
            {id: 'anthropic', kind: 'anthropic', name: 'Anthropic API', command: '', config: {model: 'claude-sonnet-5-5', apiKeyEnv: 'ANTHROPIC_API_KEY'}, enabled: false, sortOrder: 7},
        ]),
    ListMaterials: () => P(materialList),
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
Promise.all([import('./components/viewer/Viewer'), import('./components/terminal/TerminalView'), import('./components/dock/DockWorkbench'), loadCodeEditor(), loadMesh(okResult(selectedFile, 0, false).meshPath), loadParts(fakeKey(selectedFile), shotModel.parts.map((_, i) => `/parts/${fakeKey(selectedFile)}/${String(i).padStart(2, '0')}.stl`))])
    .catch(() => {})
    .then(() =>
        createRoot(document.getElementById('root')!).render(
            <StrictMode>
                <App shot={cfg.shot} />
            </StrictMode>,
        ),
    )

// Para las pruebas con CDP (scripts de revisión locales): el controlador del dock.
;(window as unknown as {__dock: typeof shotTools}).__dock = shotTools
// Idem para la sesión de impresión (prueba de clic sobre una parte).
;(window as unknown as {__print: typeof printStore}).__print = printStore
// Para las pruebas de las pestañas de proyecto con CDP: leer y mover el estado que cada pestaña conserva.
;(window as unknown as {__proj: unknown}).__proj = {editorSession, editorCommands, selectPart}

// Guard de desplazamiento igual que la app real (main.tsx).
installScrollGuard()

// settings-appearance: Configuración → General desplazada hasta el bloque de
// Apariencia (el diálogo es más alto que la ventana).
if (view === 'settings-appearance') {
    const find = window.setInterval(() => {
        const el = document.querySelector('[data-testid="appearance"]')
        if (el) {
            el.scrollIntoView({block: 'start'})
            window.clearInterval(find)
        }
    }, 100)
}

// settings-openscad-download: la descarga va por la mitad (el evento de progreso que mandaría Go).
if (view === 'settings-openscad-download') {
    window.setTimeout(() => emit('openscad:download', {phase: 'download', received: 38_400_000, total: 62_687_889, message: ''}), 400)
}

// settings-mcp: Configuración → IA desplazada hasta el bloque del servidor MCP.
if (view === 'settings-mcp') {
    const find = window.setInterval(() => {
        const el = document.querySelector('[data-testid="mcp"]')
        if (el) {
            el.scrollIntoView({block: 'start'})
            window.clearInterval(find)
        }
    }, 100)
}

// keytest=1: comprueba los atajos de tamaño con teclas simuladas (⌘ porque el
// banco corre como darwin). Cada paso se hace con el foco donde lo tendría el
// usuario; al final se lee lo que quedó aplicado.
if (q.get('keytest') === '1') {
    const press = (key: string, focusSel?: string) => {
        const el = focusSel ? document.querySelector<HTMLElement>(focusSel) : null
        if (focusSel) el?.focus()
        else (document.activeElement as HTMLElement | null)?.blur()
        const target = (document.activeElement as HTMLElement | null) ?? document.body
        target.dispatchEvent(new KeyboardEvent('keydown', {key, metaKey: true, bubbles: true, cancelable: true}))
    }
    const read = () => {
        const fs = (sel: string) => {
            const e = document.querySelector(sel)
            return e ? getComputedStyle(e).fontSize : null
        }
        return {
            ui: document.documentElement.style.getPropertyValue('--ui-font-scale'),
            chat: document.documentElement.style.getPropertyValue('--chat-font-size'),
            editor: fs('.monaco-editor .view-lines'),
            terminal: fs('.xterm-rows'),
        }
    }
    const steps: [string, string?][] = [['=' ], ['='], ['-'], ['0'], ['=', '.monaco-editor textarea'], ['=', '.xterm-helper-textarea'], ['=', '[data-font-area="chat"] textarea']]
    const log: unknown[] = []
    steps.forEach(([key, sel], i) =>
        window.setTimeout(() => {
            press(key, sel)
            window.setTimeout(() => {
                log.push({step: `${sel ?? 'body'} ${key}`, ...read()})
                document.documentElement.dataset.keytest = JSON.stringify(log)
            }, 50)
        }, 3000 + i * 400),
    )
}

// Informe de ajuste del documento: lo lee scripts/uishot-fit.sh con --dump-dom.
// El documento no puede medir más que la ventana ni estar desplazado; con
// poke=1 antes se le provoca el desplazamiento que lo rompía.
const poke = q.get('poke') === '1'
// Los contenedores con desplazamiento vertical de la barra lateral (las dos listas), elegidos por su
// estilo y no por data-lock: el chequeo no puede depender de la marca que debe comprobar.
const sideLists = () =>
    [...(document.querySelector('aside')?.children ?? [])].filter((c): c is HTMLElement => c instanceof HTMLElement && ['auto', 'scroll', 'hidden'].includes(getComputedStyle(c).overflowY) && c.tagName === 'DIV' && c.scrollHeight > 0 && !c.className.includes('h-px'))
let pokes = 0
const report = () => {
    const d = document.documentElement
    if (poke && pokes++ < 10) {
        // Un hijo que desborda la ventana (lo que hace un contenido más
        // ancho/alto que ella): sin esto no hay nada que desplazar.
        if (!document.getElementById('poke-overflow')) {
            const o = document.createElement('div')
            o.id = 'poke-overflow'
            o.style.cssText = 'width:3000px;height:3000px;pointer-events:none'
            document.getElementById('root')!.appendChild(o)
        }
        window.scrollTo(37, 53)
        d.scrollTop = 41
        d.scrollLeft = 29
        document.body.scrollTop = 17
        document.getElementById('root')!.scrollTop = 23
        document.querySelector<HTMLElement>('.xterm-helper-textarea')?.focus()
        document.querySelector<HTMLElement>('header')?.scrollIntoView()
        // La barra lateral: sus listas solo se desplazan en vertical. Se las empuja de lado, se
        // enfocan los nodos del árbol, se abre el renombrado en línea con un texto larguísimo y se
        // pide scrollIntoView horizontal: nada de eso puede dejar un scrollLeft distinto de 0.
        sideLists().forEach((el, i) => (el.scrollLeft = 25 + i))
        const rows = [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')]
        rows.forEach((r) => r.focus())
        rows.at(-1)?.scrollIntoView({inline: 'center', block: 'nearest'})
        if (pokes === 3) rows.at(-1)?.dispatchEvent(new KeyboardEvent('keydown', {key: 'F2', bubbles: true, cancelable: true}))
        if (pokes >= 5) {
            const inp = document.querySelector<HTMLInputElement>('[role="tree"] input')
            if (inp) {
                inp.value = 'un nombre de renombrado larguísimo que no cabe en la barra lateral de ninguna manera posible ni imaginable'
                inp.focus()
                inp.setSelectionRange(inp.value.length, inp.value.length)
            }
        }
    }
    const bar = document.querySelector('footer')?.getBoundingClientRect()
    const headEl = document.querySelector('header')
    const head = headEl?.getBoundingClientRect()
    d.dataset.docfit = JSON.stringify({
        iw: innerWidth, ih: innerHeight,
        sw: d.scrollWidth, sh: d.scrollHeight,
        sx: d.scrollLeft + document.body.scrollLeft + scrollX, sy: d.scrollTop + document.body.scrollTop + scrollY,
        rootTop: document.getElementById('root')!.scrollTop,
        headerTop: head ? Math.round(head.top) : null, headerLeft: head ? Math.round(head.left) : null,
        // La cabecera recorta lo que desborda (overflow-clip): si su contenido es más ancho que ella
        // (pestañas de proyecto que no se acortan), los botones de la derecha quedan fuera de la ventana.
        headerSw: headEl ? headEl.scrollWidth : null, headerCw: headEl ? headEl.clientWidth : null,
        // Barra lateral: ningún contenedor marcado puede quedar desplazado, y las listas no pueden
        // tener contenido más ancho que su caja (scrollWidth > clientWidth = nombres cortados sin
        // forma de verlos). `toggleLeft` es el botón de la barra lateral: en macOS los semáforos
        // ocupan hasta ~72 px y no deben tapárselo.
        lockMoved: [...document.querySelectorAll<HTMLElement>('[data-lock]')]
            .filter((el) => el.scrollLeft !== 0 || (el.dataset.lock !== 'x' && el.scrollTop !== 0))
            .map((el) => el.tagName.toLowerCase() + (el.dataset.lock ? `[${el.dataset.lock}]` : '')),
        sideBoxes: sideLists().map((el) => ({sw: el.scrollWidth, cw: el.clientWidth, sl: el.scrollLeft})),
        sideTitleLeft: (() => {
            const r = document.querySelector('aside span')?.getBoundingClientRect()
            return r ? Math.round(r.left) : null
        })(),
        toggleLeft: (() => {
            const r = document.querySelector('header button')?.getBoundingClientRect()
            return r ? Math.round(r.left) : null
        })(),
        platform,
        footerBottom: bar ? Math.round(bar.bottom) : null,
        footerRight: bar ? Math.round(bar.right) : null,
    })
}
window.setInterval(report, 200)
new MutationObserver(report).observe(document.body, {childList: true, subtree: true})
