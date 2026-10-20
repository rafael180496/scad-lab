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
//   click  selector CSS de un elemento al que hacerle clic antes de fotografiar (UISHOT_CLICK en uishot.sh): para revisar un
//          menú o un desplegable de cualquier vista sin escribir una vista nueva
//   keytest 1 = simula ⌘+ ⌘- ⌘0 (interfaz) y ⌘+ con el foco en el editor, la terminal
//          y el chat; el resultado queda en data-keytest
//   dockraw  texto crudo del diseño de paneles guardado (prueba de diseño roto o ajeno)
//   vh     alto de la ventana en px (lo pone uishot.sh): fija el alto de la página para que el dock no se reacomode al fotografiar
//   poke   1 = intenta desplazar el documento (scrollTo, focus, scrollIntoView)
//          para comprobar que no se mueve; el resultado queda en data-docfit
//   persist  reset | 1 = «cerrar y abrir»: los bindings de guardado escriben en localStorage
//          (uishot.persist.v1) y GetBootstrap / ListOpenProjects / OpenProject leen de ahí.
//          `reset` parte de cero; recargar con `1` es abrir la app otra vez con lo guardado
//          (scripts/uishot-persist.sh lo maneja por CDP)
//
// Una vista nueva = una entrada en VIEWS + su fila en scripts/help-shots.sh.
import {StrictMode, Suspense, lazy} from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App, {type ShotOptions} from './App'
import type {SettingsTab} from './components/settings/SettingsDialog'
import {loadMesh, loadParts, loadRawMesh} from './lib/mesh'
import {loadCodeEditor, loadDiffViewer, loadSkillCodeEditor} from './components/editor/lazy'
import {fmt, setLanguage, t as dict} from './i18n'
import {ConfirmHost} from './components/ui/Dialog'
import {Toaster, toast} from './components/ui/Toast'
import {VaultHost} from './components/vault/VaultHost'
import {APPEARANCE_DEFAULTS, applyAppearance} from './lib/appearance'
import {setPlatform} from './lib/platform'
import {applyTheme} from './lib/theme'
import {terminalShot} from './components/terminal/shot'
import {shotTools} from './components/dock/dock'
import {printStore, selectPart} from './lib/printSession'
import {editorCommands, editorSession} from './lib/editorSession'
import {installScrollGuard} from './lib/scrollGuard'
import {skillsShot} from './lib/skillsShot'
import {prepareSession} from './lib/meshEditSession'
import {slugify} from './lib/slug'
import {fromImageShot} from './lib/fromImageShot'
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

// S12.c. `workspace-starter`: un workspace recién abierto con SOLO el modelo de inicio (001) en el visor.
// `chat-agent-creates` y `chat-autosave`: un workspace que acaba de recibir su primer checkpoint desde el chat (con una
// herramienta, y con el guardado automático). `chat-query-only`: «Solo consulta» con «Crear checkpoint y renderizar» y el
// aviso «Cambiar a Agente».
const starterShot = view === 'workspace-starter'
const createsShot = view === 'chat-agent-creates' || view === 'chat-autosave' || view === 'chat-agy-denied'
const firstShot = starterShot || createsShot

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
    chat: {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    // S12: Hermes con herramientas. Los pasos de un turno (render con error de línea → corrige → preview con
    // imagen), los adjuntos pendientes (una foto y un STL con miniatura) y la lista de conversaciones con una en
    // renombrado en línea.
    'chat-agent-steps': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    'chat-attachments': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    'chat-conversations': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    // S12.b: una respuesta de texto en «Solo consulta» con Markdown (tabla, títulos, listas, enlace) y su bloque OpenSCAD con
    // «Abrir en el editor» y «Guardar como checkpoint».
    'chat-code': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    // S12.c: el chat termina siempre en un .scad renderizado. `chat-agent-creates`: un workspace vacío, el pedido y los pasos
    // create_checkpoint → render → preview con el enlace al checkpoint y la pieza en el visor. `chat-autosave`: un modelo sin
    // herramientas contesta con el bloque y ScadLab lo guarda («Guardado automáticamente»). `chat-query-only`: «Solo consulta»
    // con «Crear checkpoint y renderizar» y el aviso «Cambiar a Agente». `workspace-starter`: un workspace recién abierto con
    // el modelo de inicio (001) ya en el visor.
    'chat-agent-creates': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-autosave': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-query-only': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    // S12.c T14: Antigravity bloqueó una herramienta de ScadLab por permisos (en --print termina el turno): el mensaje
    // dice cuál, dónde aprueba ScadLab sus herramientas y qué hacer, y el aviso trae «Reparar permisos de Antigravity».
    'chat-agy-denied': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'workspace-starter': {scenario: 'ok'},
    // S13 (editar un STL): el diálogo de importar con una malla abierta, el modo «Editar» con el gizmo de mover y la pila,
    // el plano de corte partiendo en dos colores, un cilindro como agujero (rojo translúcido) y «Modificar con IA» (el
    // chat con el modelo adjunto y el texto sugerido). `mesh-edit-w320/480/720` es el editor solo, en un panel de ese ancho.
    'mesh-import': {scenario: 'ok', shot: {meshImport: {path: '/Users/maker/Downloads/soporte-abierto.stl'}}},
    'mesh-edit': {
        scenario: 'ok',
        shot: {
            dock: 'hidden',
            meshEdit: {
                tool: 'translate',
                select: 'model',
                view: 'iso',
                steps: [
                    {op: 'move', to: [-12, -62, 0]},
                    {op: 'add', shape: 'cube', mode: 'union', at: [26, 0, 31.6], size: [20, 14, 8], color: '#ff7a1a'},
                    {op: 'move', to: [-24, -62, 0]},
                ],
            },
        },
    },
    'mesh-cut': {
        scenario: 'ok',
        shot: {
            dock: 'hidden',
            meshEdit: {
                tool: 'translate',
                select: 'cut',
                view: 'iso',
                steps: [{op: 'cut', point: [0, 0, 13.8], normal: [0, 0, 1], keep: 'both', split: 'colors', colors: ['#ff7a1a', '#5aa2ff']}],
            },
        },
    },
    'mesh-hole': {
        scenario: 'ok',
        shot: {
            dock: 'hidden',
            meshEdit: {
                tool: 'translate',
                select: 'part:0',
                view: 'iso',
                steps: [{op: 'add', shape: 'cylinder', mode: 'hole', at: [0, 0, 13.8], size: [14, 14, 40]}],
            },
        },
    },
    'mesh-ai': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat', meshAI: '006-two-color-logo.scad'}},
    // S12.b: el chat nuevo con sugerencias (chat-empty), el selector de proveedor abierto con los CLIs detectados y
    // un servidor (chat-providers), los modelos de un CLI (chat-cli-models) y el aviso sin ningún proveedor.
    'chat-empty': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    // S15, modelo desde una imagen: el diálogo «Crear desde una imagen» con dos fotos, la medida y qué es (sobre el chat vacío);
    // la tarjeta de `compare_with_image` (la foto y el modelo lado a lado con la vista, las proporciones y el IoU) y la de
    // `trace_image_outline` (la silueta trazada y el SVG que dejó). Las fotos son imágenes sintéticas de frontend/public/uishot/.
    'chat-from-image': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    'chat-compare': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    'chat-trace': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    // Una foto adjunta con un modelo que no ve imágenes: el aviso con «Cambiar a un modelo con visión» y «Usar trazado de contorno».
    'chat-no-vision': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
    'chat-providers': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-cli-models': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    'chat-no-providers': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    // S12: el selector de modelo abierto (búsqueda, insignias de visión y herramientas, precios de OpenRouter).
    'chat-models': {scenario: 'ok', shot: {panel: 'chat', dock: 'tall'}},
    // S12: el formulario «Agregar servidor» con el atajo de OpenRouter elegido.
    'settings-providers-add': {scenario: 'ok', shot: {settingsTab: 'providers'}},
    // S12.b: Configuración → Proveedores con los CLIs detectados, y el diálogo de «Importar servidores de mini-tools».
    'settings-import-minitools': {scenario: 'ok', shot: {settingsTab: 'providers'}},
    // S12 (12.10): el llavero propio. «Crear contraseña maestra» aparece al guardar la primera clave de API (sobre
    // Proveedores de IA, con el formulario de OpenRouter detrás); «Desbloquear» cuando la llave no se recuperó sola
    // y hace falta una clave (sobre el chat).
    'vault-create': {scenario: 'ok', shot: {settingsTab: 'providers'}},
    'vault-unlock': {scenario: 'ok', shot: {panel: 'chat', dock: 'chat'}},
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
    // Papelera (borrado permanente): los dos diálogos de confirmación, y la de mandar a la papelera un checkpoint con hijos.
    'trash-empty-confirm': {scenario: 'ok', shot: {trash: 'empty'}},
    'tree-trash-confirm': {scenario: 'ok', shot: {treeConfirm: '002-sliding-lid.scad'}},
    // Un hijo cuyo padre está en la papelera: el ⚠ del árbol y, en Problemas, el aviso con «Hacer principal».
    'problems-orphan': {scenario: 'ok', shot: {select: '007-snap-case-v2.scad', panel: 'problems'}},
    'diff-parent': {scenario: 'ok', shot: {select: '006-two-color-logo.scad', compare: '006-two-color-logo.scad', panel: 'source', dock: 'diff'}},
    // Historial de versiones (S16): la IA editó 003 y 006 en el lugar. `history-menu`: el árbol con el indicador «Editado por la
    // IA», el menú del nodo con «Deshacer cambio de la IA», «Ver diferencias» e «Historial», y los chips de las cabeceras.
    // `history-dialog`: la lista de versiones. `history-diff`: la versión anterior frente a la actual.
    'history-menu': {scenario: 'ok', shot: {select: '006-two-color-logo.scad', treeMenu: '006-two-color-logo.scad'}},
    'history-dialog': {scenario: 'ok', shot: {select: '006-two-color-logo.scad', history: {kind: 'history', file: '006-two-color-logo.scad'}}},
    'history-diff': {scenario: 'ok', shot: {select: '006-two-color-logo.scad', history: {kind: 'diff', file: '006-two-color-logo.scad'}}},
    // Pestañas de proyecto (S8): tres proyectos abiertos (uno con un render en curso y
    // cambios sin guardar, otro con un agente vivo y su último render fallido), el menú «+»
    // y la barra con poco ancho (se captura a 1024 px).
    'projects-tabs': {scenario: 'ok', shot: {unsavedTabs: [2]}},
    'projects-plus-menu': {scenario: 'ok', shot: {projectsMenu: 'plus', unsavedTabs: [2]}},
    'projects-narrow': {scenario: 'ok', shot: {unsavedTabs: [2]}},
    empty: {scenario: 'empty'},
    // «Nuevo workspace» con la ubicación elegida y la vista previa de la ruta final.
    newworkspace: {scenario: 'empty', shot: {newWorkspace: {name: es ? 'Soporte de auriculares' : 'Headphone stand', parent: '/Users/maker/Documents/Modelos 3D/Impresiones'}}},
    // Impresoras multimarca y configuración por workspace (S14): «Nuevo workspace» con una Prusa MK4S, cuatro colores (MMU3) y sus
    // skills; el selector abierto con las marcas agrupadas; y «Configuración del workspace» de un workspace con una Creality K1 Max
    // (sección Impresora con su ficha; `-skills` y `-print` son las otras dos secciones, para revisarlas).
    'workspace-new-printer': {scenario: 'empty', shot: {newWorkspace: {name: es ? 'Soporte para auriculares' : 'Headphone stand', parent: '/Users/maker/Documents/Modelos 3D/Impresiones', printer: 'prusa-mk4s', colors: 4}}},
    'printer-picker': {scenario: 'empty', shot: {newWorkspace: {name: es ? 'Soporte para auriculares' : 'Headphone stand', parent: '/Users/maker/Documents/Modelos 3D/Impresiones', printer: 'prusa-mk4s', printerOpen: true}}},
    'workspace-config': {scenario: 'ok', shot: {workspaceConfig: {section: 'printer'}}},
    'workspace-config-skills': {scenario: 'ok', shot: {workspaceConfig: {section: 'skills'}}},
    'workspace-config-print': {scenario: 'ok', shot: {workspaceConfig: {section: 'print'}}},
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
for (const tab of ['general', 'openscad', 'ai', 'providers', 'printing', 'skills', 'about'] as SettingsTab[]) {
    VIEWS[`settings-${tab}`] = {scenario: 'ok', shot: {settingsTab: tab}}
}
// Skills (Configuración → Skills): la lista con sus botones, el editor de una skill propia (y con un error
// de frontmatter), una incluida en solo lectura, la confirmación de borrado y el cambio de carpeta.
const SKILLS_VIEWS: Record<string, NonNullable<typeof skillsShot.mode>> = {
    'skills-editor': 'editor',
    'skills-editor-errors': 'editor-errors',
    'skills-view': 'editor-builtin',
    'skills-delete': 'delete',
    'skills-folder': 'folder',
}
for (const [v, mode] of Object.entries(SKILLS_VIEWS)) VIEWS[v] = {scenario: 'ok', shot: {settingsTab: 'skills'}}
skillsShot.mode = SKILLS_VIEWS[view]
// S13: el editor de STL solo, en un panel de N px (mesh-edit-w320…), para ver cómo se pliega y comprobar que nada desborda.
const meshWidth = Number(/^mesh-edit-w(\d+)$/.exec(view)?.[1] ?? 0)
const meshHarness = meshWidth > 0
const cfg = VIEWS[view] ?? (meshHarness ? VIEWS['mesh-edit'] : VIEWS.main)
// S15: el diálogo «Crear desde una imagen» ya abierto, con dos fotos inventadas (lib/fromImageShot.ts).
if (view === 'chat-from-image') {
    fromImageShot.open = true
    fromImageShot.reference = es ? 'El ancho total es 60 mm; la moneda de 25 centavos mide 24 mm' : 'The total width is 60 mm; the 25-cent coin is 24 mm across'
    fromImageShot.what = es ? 'Una escuadra en L de metal para sostener un estante' : 'A metal L-bracket to hold up a shelf'
    fromImageShot.photos = [
        {id: 'att-front', name: es ? 'escuadra-frente.jpg' : 'bracket-front.jpg', size: 1_420_000, thumb: '/uishot/photo-bracket-front.png'},
        {id: 'att-side', name: es ? 'escuadra-lado.jpg' : 'bracket-side.jpg', size: 960_000, thumb: '/uishot/photo-bracket-side.png'},
    ]
}
// S12.b: el chat solo, en un panel de N px de ancho (chat-narrow = 360), para ver y comprobar que la barra no se desborda.
const chatWidth = view === 'chat-narrow' ? 360 : Number(/^chat-w(\d+)$/.exec(view)?.[1] ?? 0)
const chatHarness = chatWidth > 0
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
// Skills propias (inventadas): dónde viven y el contenido de las que se abren en el editor.
const sep = platform === 'windows' ? '\\' : '/'
const skillsDir = platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\ScadLab\\skills' : '/Users/maker/Library/Application Support/ScadLab/skills'
const gearDescription = es ? 'Engranajes helicoidales con el módulo de mi taller.' : 'Helical gears using my workshop module.'
const gearSkillMd = es
    ? `---
name: gear-helper
description: ${gearDescription} Úsala cuando el usuario pida engranajes, piñones o cremalleras.
---

# Engranajes del taller

Todos mis engranajes usan módulo 1,5 y ángulo de presión de 20°.

## Reglas

- Parte siempre de \`templates/engranaje.scad\`.
- Holgura entre dientes: 0,2 mm en la A1 mini.
- Cada versión es un checkpoint nuevo \`NNN-slug.scad\`.

## Flujo

1. Pregunta el número de dientes y el ancho.
2. Copia la plantilla como checkpoint nuevo y ajusta los parámetros.
3. Renderiza y revisa que los dientes engranen.
`
    : `---
name: gear-helper
description: ${gearDescription} Use it when the user asks for gears, pinions or racks.
---

# Workshop gears

All my gears use module 1.5 and a 20° pressure angle.

## Rules

- Always start from \`templates/engranaje.scad\`.
- Tooth clearance: 0.2 mm on the A1 mini.
- Every version is a new \`NNN-slug.scad\` checkpoint.

## Flow

1. Ask for the number of teeth and the width.
2. Copy the template as a new checkpoint and adjust the parameters.
3. Render and check that the teeth mesh.
`
const coreSkillMd = `---
name: openscad-core
description: ${es ? 'Reglas de modelado paramétrico y del workspace.' : 'Parametric modeling and workspace rules.'}
---

# OpenSCAD core

${es ? 'Cada versión de un modelo es un checkpoint nuevo; nunca se sobrescribe un .scad.' : 'Every version of a model is a new checkpoint; a .scad is never overwritten.'}
`
const gearScad = `// @checkpoint parent= desc="Engranaje base"
teeth = 24;   // [8:1:80]
module_mm = 1.5;
width = 8;

color("#ff7a1a") cylinder(d = teeth * module_mm, h = width, $fn = teeth * 4);
`

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

// S14: las vistas de configuración por workspace usan una impresora multicolor de otra marca (Creality K1 Max con CFS).
const printerWsShot = view === 'workspace-config' || view === 'workspace-config-skills' || view === 'workspace-config-print'
// Pestañas de proyecto (S8): tres proyectos abiertos y un cuarto workspace sin abrir.
const projectsShot = view.startsWith('projects-')
const sidebarLong = view === 'sidebar-long'
const withLast = <T extends {id: number}>(w: T) => ({...w, lastCheckpoint: persisted?.last[w.id] ?? ''})
const workspaces = (
    cfg.scenario === 'empty'
        ? []
        : [
              {id: 1, name: sidebarLong ? (es ? 'Caja de la Raspberry Pi 5 con un nombre de workspace realmente largo para probar el recorte' : 'Raspberry Pi 5 case with a really long workspace name to test the clipping') : starterShot ? (es ? 'Mi primer proyecto' : 'My first project') : es ? 'Caja Raspberry Pi 5' : 'Raspberry Pi 5 case', path: `${home}/${starterShot ? 'my-first-project' : 'rpi5-case'}`, printerId: printerWsShot ? 'creality-k1-max' : 'bambu-a1-mini', description: '', lastOpened: 0, createdAt: 0},
              {id: 2, name: es ? 'Soporte de auriculares' : 'Headphone stand', path: `${home}/headphone-stand`, printerId: 'bambu-a1', description: '', lastOpened: 0, createdAt: 0},
              {id: 3, name: es ? 'Organizador Gridfinity' : 'Gridfinity organizer', path: `${home}/gridfinity`, printerId: 'bambu-p1s', description: '', lastOpened: 0, createdAt: 0},
              ...(projectsShot
                  ? [{id: 4, name: es ? 'Llavero personalizable' : 'Custom keychain', path: `${home}/keychain`, printerId: 'bambu-a1-mini', description: '', lastOpened: 0, createdAt: 0}]
                  : []),
          ]
).map(withLast)

// Vínculos y rótulos del árbol (S3): solo en las vistas de checkpoints avanzados,
// para que el resto de las capturas conserve el árbol de siempre.
const treeShot = ['tree-menu', 'tree-active-branch', 'trash', 'trash-empty-confirm', 'tree-trash-confirm', 'problems-orphan', 'diff-parent'].includes(view)
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
const activeParent = view === 'tree-active-branch' || view === 'trash' || view === 'trash-empty-confirm' ? '003-vent-slots.scad' : ''
// S16: en las vistas del historial, la IA editó 003 y 006 en el lugar (su versión anterior está en el historial).
const historyShot = view.startsWith('history-')
const cp = (n: number, file: string, parent: string, desc: string) => ({
    id: n, workspaceId: 1, fileName: file, parentFile: parent, label: '', description: desc,
    sessionId: '', agentId: '', chatId: 0, collapsed: false, fileMtime: treeShot ? 1792080000 + n * 5400 : 0, createdAt: 0,
    editedByAI: historyShot && (file === '003-vent-slots.scad' || file === '006-two-color-logo.scad'),
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
// El primer checkpoint de un workspace sin nada más: el modelo de inicio, o el que creó el chat.
const firstFile = starterShot ? `001-${slugify(workspaces[0]?.name ?? '')}.scad` : es ? '001-caja-rpi5.scad' : '001-rpi5-case.scad'
const firstCheckpoints = [cp(1, firstFile, '', starterShot ? (workspaces[0]?.name ?? '') : es ? 'Caja con tapa a presión para una Raspberry Pi 5' : 'Snap-fit box for a Raspberry Pi 5')]
const checkpoints = sidebarLong ? longCheckpoints : firstShot ? firstCheckpoints : [
    cp(1, '001-base-box.scad', '', es ? 'Caja base 92×64×32' : 'Base box 92×64×32'),
    cp(2, '002-sliding-lid.scad', '001-base-box.scad', es ? 'Tapa deslizante' : 'Sliding lid'),
    cp(3, '003-vent-slots.scad', '002-sliding-lid.scad', es ? 'Ranuras de ventilación' : 'Vent slots'),
    cp(4, '004-usb-c-cutout.scad', '003-vent-slots.scad', es ? 'Recorte USB-C y HDMI' : 'USB-C and HDMI cutouts'),
    cp(5, '005-snap-lid.scad', '002-sliding-lid.scad', es ? 'Rama: tapa a presión' : 'Branch: snap-fit lid'),
    cp(6, '006-two-color-logo.scad', '005-snap-lid.scad', es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS'),
    // `problems-orphan`: su padre (008) se mandó a la papelera con una versión anterior de la app.
    ...(view === 'problems-orphan' ? [cp(7, '007-snap-case-v2.scad', '008-draft-lid.scad', es ? 'Caja a presión, segunda versión' : 'Snap-fit case, second version')] : []),
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
const selectedFile = sidebarLong ? checkpoints.at(-1)!.fileName : firstShot ? firstFile : cfg.scenario === 'error' ? '004-usb-c-cutout.scad' : '006-two-color-logo.scad'

const mainSource = `// @checkpoint parent=005-snap-lid.scad desc="${es ? 'Logo bicolor para el AMS' : 'Two-color logo for the AMS'}"
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

// El modelo de inicio (assets/workspace/starter.scad.*.tmpl, recortado) y el primer checkpoint que crea el chat.
const starterSource = `// @checkpoint parent= desc="${es ? 'Mi primer proyecto' : 'My first project'}"
// ${es ? 'Punto de partida de ScadLab: una base con chaflán y un pomo encima.' : "ScadLab's starting point: a chamfered base with a knob on top."}
// ${es ? 'Cambia las medidas en el Customizer (panel de parámetros) o escribe tu propio\n// modelo: si le pides cambios a la IA sobre este archivo, los edita aquí mismo\n// y guarda antes la versión anterior en su historial (se puede deshacer).' : 'Change the sizes in the Customizer (parameters panel) or write your own\n// model: if you ask the AI to change this file, it edits it right here and first saves\n// the previous version in its history (you can undo).'}

/* [Base] */
base_width = 50;    // [20:1:150]
base_depth = 50;    // [20:1:150]
base_height = 6;    // [2:0.5:20]
corner_radius = 6;  // [2:0.5:25]

/* [${es ? 'Pomo' : 'Knob'}] */
knob_diameter = 22; // [8:1:60]
knob_height = 14;   // [6:1:40]

/* [${es ? 'Colores' : 'Colors'}] */
base_color = "#2F6FDE";
knob_color = "#F2B134";

color(base_color) base(base_width, base_depth, base_height, corner_radius);
color(knob_color) translate([0, 0, base_height]) knob(knob_diameter, knob_height);
`
const createdSource = `// @checkpoint parent= desc="${es ? 'Caja con tapa a presión para una Raspberry Pi 5' : 'Snap-fit box for a Raspberry Pi 5'}"
board_w = 85;   // [60:1:120]
board_d = 56;   // [40:1:90]
wall    = 2;    // [1.2:0.2:4]
height  = 24;   // [16:1:48]

color("#2b2f38") difference() {
    cube([board_w + 2*wall, board_d + 2*wall, height]);
    translate([wall, wall, wall]) cube([board_w, board_d, height]);
}
`
const source = starterShot ? starterSource : createsShot ? createdSource : mainSource

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
        : firstShot
          ? {[firstFile]: okResult(firstFile, starterShot ? 410 : 520, false)}
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
            : file === '007-snap-case-v2.scad'
              ? source.replace('parent=005-snap-lid.scad', 'parent=008-draft-lid.scad')
              : source
        : `// ${workspaces.find((w) => w.id === id)?.name ?? ''} · ${file}\nwidth = ${40 + id * 7};   // [20:1:120]\ncube([width, width / 2, 10]);\n`

// Lo que devolvería LintCheckpointSource (backend/scad) sobre cada código:
// las líneas coinciden con los fuentes de arriba.
const diag = (severity: string, line: number, col: number, endCol: number, code: string, msg: [string, string]) => ({
    severity, line, col, endLine: line, endCol, code, source: code === 'syntax' ? 'openscad' : 'scadlab', message: es ? msg[0] : msg[1],
})
const lintFor = (file: string) =>
    file === '007-snap-case-v2.scad'
        ? [
              diag('warning', 1, 1, 0, 'SL002', [
                  'parent=008-draft-lid.scad está en la papelera: restáuralo o haz principal a este checkpoint',
                  'parent=008-draft-lid.scad is in the trash: restore it or make this checkpoint a root',
              ]),
          ]
        : file === '004-usb-c-cutout.scad'
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

// Catálogo de impresoras (S14): INVENTADO pero con las cifras reales de la ficha de cada fabricante (volumen, boquilla,
// temperaturas máximas, sistema multicolor). `max` = multicolor_slots del catálogo, `def` = default_colors. El orden de
// marcas es el de la app: Bambu Lab, Prusa, Creality, Elegoo, Anycubic, Snapmaker, Ultimaker.
type ShotPrinter = {
    id: string; name: string; model: string; brand: string; vol: [number, number, number]; nozzle?: number; nozzles?: number; fil?: number
    kind: string; sys: string; max: number; def: number; closed?: boolean; heated?: boolean; bed: string; tn: number; tb: number
    slicers: string[]; verify?: string[]; src: string
}
const BRANDS = [
    {id: 'bambu', name: 'Bambu Lab', skill: 'bambu-lab-printers'},
    {id: 'prusa', name: 'Prusa Research', skill: 'prusa-printers'},
    {id: 'creality', name: 'Creality', skill: 'creality-printers'},
    {id: 'elegoo', name: 'Elegoo', skill: 'elegoo-printers'},
    {id: 'anycubic', name: 'Anycubic', skill: 'anycubic-printers'},
    {id: 'snapmaker', name: 'Snapmaker', skill: 'snapmaker-printers'},
    {id: 'ultimaker', name: 'UltiMaker', skill: 'ultimaker-printers'},
]
const SHOT_PRINTERS: ShotPrinter[] = [
    {id: 'bambu-a1-mini', name: 'Bambu Lab A1 mini', model: 'A1 mini', brand: 'bambu', vol: [180, 180, 180], kind: 'ams', sys: 'AMS lite', max: 16, def: 4, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 300, tb: 80, slicers: ['bambu-studio', 'orcaslicer'], src: 'https://bambulab.com/en/a1-mini'},
    {id: 'bambu-p1s', name: 'Bambu Lab P1S', model: 'P1S', brand: 'bambu', vol: [256, 256, 256], kind: 'ams', sys: 'AMS', max: 16, def: 4, closed: true, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 300, tb: 100, slicers: ['bambu-studio', 'orcaslicer'], src: 'https://bambulab.com/en/p1'},
    {id: 'bambu-x1c', name: 'Bambu Lab X1 Carbon', model: 'X1C', brand: 'bambu', vol: [256, 256, 256], kind: 'ams', sys: 'AMS', max: 16, def: 4, closed: true, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 0, tb: 0, slicers: ['bambu-studio', 'orcaslicer'], verify: ['max_nozzle_temp_c', 'max_bed_temp_c'], src: 'https://bambulab.com/en/x1'},
    {id: 'bambu-h2d', name: 'Bambu Lab H2D', model: 'H2D', brand: 'bambu', vol: [325, 320, 325], nozzles: 2, kind: 'ams', sys: 'AMS 2 Pro / AMS HT', max: 25, def: 4, closed: true, heated: true, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 350, tb: 120, slicers: ['bambu-studio', 'orcaslicer'], src: 'https://bambulab.com/en/h2d'},
    {id: 'prusa-mk4s', name: 'Prusa MK4S', model: 'MK4S', brand: 'prusa', vol: [250, 210, 220], fil: 1.75, kind: 'mmu', sys: 'Prusa MMU3', max: 5, def: 1, bed: (es ? 'Hoja de acero con PEI' : 'PEI spring-steel sheet'), tn: 290, tb: 120, slicers: ['prusaslicer', 'orcaslicer'], src: 'https://www.prusa3d.com/product/original-prusa-mk4s-3d-printer-5/'},
    {id: 'prusa-core-one', name: 'Prusa CORE One+', model: 'CORE One+', brand: 'prusa', vol: [250, 220, 270], fil: 1.75, kind: 'mmu', sys: 'Prusa MMU3 / INDX', max: 5, def: 1, closed: true, heated: true, bed: (es ? 'Hoja de acero con PEI' : 'PEI spring-steel sheet'), tn: 290, tb: 120, slicers: ['prusaslicer', 'orcaslicer'], src: 'https://www.prusa3d.com/product/prusa-core-one/'},
    {id: 'prusa-mini', name: 'Prusa MINI+', model: 'MINI+', brand: 'prusa', vol: [180, 180, 180], fil: 1.75, kind: 'none', sys: '', max: 1, def: 1, bed: (es ? 'Hoja de acero con PEI' : 'PEI spring-steel sheet'), tn: 280, tb: 100, slicers: ['prusaslicer', 'orcaslicer'], src: 'https://www.prusa3d.com/product/original-prusa-mini-semi-assembled-3d-printer-9/'},
    {id: 'creality-k1-max', name: 'Creality K1 Max', model: 'K1 Max', brand: 'creality', vol: [300, 300, 300], fil: 1.75, kind: 'cfs', sys: 'Creality CFS-C', max: 4, def: 1, closed: true, bed: (es ? 'PEI flexible' : 'Flexible PEI'), tn: 300, tb: 100, slicers: ['creality-print', 'orcaslicer'], src: 'https://www.creality.com/support/k1-max'},
    {id: 'creality-ender-3-v3', name: 'Creality Ender-3 V3', model: 'Ender-3 V3', brand: 'creality', vol: [220, 220, 250], fil: 1.75, kind: 'none', sys: '', max: 1, def: 1, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 300, tb: 110, slicers: ['creality-print', 'orcaslicer'], src: 'https://www.creality.com/support/ender-3-v3'},
    {id: 'elegoo-centauri-carbon', name: 'Elegoo Centauri Carbon', model: 'Centauri Carbon', brand: 'elegoo', vol: [256, 256, 256], fil: 1.75, kind: 'cfs', sys: 'Elegoo CANVAS', max: 4, def: 1, closed: true, bed: (es ? 'PEI texturizado' : 'Textured PEI'), tn: 320, tb: 110, slicers: ['elegoo-slicer', 'orcaslicer'], src: 'https://www.elegoo.com/products/centauri-carbon'},
    {id: 'anycubic-kobra-3', name: 'Anycubic Kobra 3', model: 'Kobra 3', brand: 'anycubic', vol: [250, 250, 260], fil: 1.75, kind: 'ace', sys: 'Anycubic ACE Pro', max: 8, def: 4, bed: (es ? 'PEI flexible' : 'Flexible PEI'), tn: 300, tb: 110, slicers: ['anycubic-slicer-next', 'orcaslicer'], src: 'https://store.anycubic.com/products/kobra-3-combo'},
    {id: 'snapmaker-u1', name: 'Snapmaker U1', model: 'U1', brand: 'snapmaker', vol: [270, 270, 270], nozzles: 4, fil: 1.75, kind: 'toolchanger', sys: (es ? '4 cabezales independientes' : '4 independent toolheads'), max: 4, def: 4, bed: (es ? 'PEI flexible' : 'Flexible PEI'), tn: 300, tb: 100, slicers: ['snapmaker-orca', 'orcaslicer'], src: 'https://www.snapmaker.com/snapmaker-u1'},
    {id: 'ultimaker-s7', name: 'UltiMaker S7', model: 'S7', brand: 'ultimaker', vol: [330, 240, 300], fil: 2.85, kind: 'dual', sys: (es ? 'Doble extrusión (dos print cores)' : 'Dual extrusion (two print cores)'), max: 2, def: 2, closed: true, bed: (es ? 'Vidrio' : 'Glass'), tn: 280, tb: 0, slicers: ['ultimaker-cura'], verify: ['max_bed_temp_c'], src: 'https://ultimaker.com/3d-printers/s-series/ultimaker-s7/'},
]
const printers = SHOT_PRINTERS.map((p) => {
    const brand = BRANDS.find((b) => b.id === p.brand)!
    return {
        id: p.id, name: p.name, model: p.model, brand_id: brand.id, brand_name: brand.name, skill: brand.skill,
        build_volume: {x: p.vol[0], y: p.vol[1], z: p.vol[2]}, nozzle_mm: p.nozzle ?? 0.4, nozzle_count: p.nozzles ?? 1, filament_diameter_mm: p.fil ?? 1.75,
        multicolor_kind: p.kind, multicolor_system: p.sys, multicolor_slots: p.max, max_colors: p.max, default_colors: p.def,
        enclosed: !!p.closed, heated_chamber: !!p.heated, bed_surface: p.bed, max_nozzle_temp_c: p.tn, max_bed_temp_c: p.tb,
        slicers: p.slicers, notes: '', sources: [p.src], verify: p.verify ?? [],
    }
})

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
    // S14: una ruta fijada a mano (la de Creality Print) para que Configuración → Impresión la muestre.
    slicerPaths: {'creality-print': platform === 'windows' ? 'C:\\Tools\\Creality Print\\CrealityPrint.exe' : '/Applications/Creality Print.app'} as Record<string, string>,
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
// El modelo de inicio (workspace-starter): una base azul y un pomo ámbar, generados con la geometría de starter.scad.*.tmpl
// (frontend/public/uishot/starter-*.stl, generados una vez con un script de Python a partir de esas medidas).
const STARTER = {
    all: {triangles: 1232, min: [-25, -25, 0], max: [25, 25, 20], size: [50, 50, 20], volumeMm3: 19415, areaMm2: 7563.6, watertight: true},
    base: {triangles: 176, min: [-25, -25, 0], max: [25, 25, 6], size: [50, 50, 6], volumeMm3: 14811.8, areaMm2: 6075.2, watertight: true},
    knob: {triangles: 1056, min: [-11, -11, 6], max: [11, 11, 20], size: [22, 22, 14], volumeMm3: 4603.2, areaMm2: 1488.3, watertight: true},
}
const starterModel: ShotModel = {
    mesh: '/uishot/starter-all.stl',
    info: STARTER.all,
    parts: [
        {name: 'Color 1 (#2F6FDE)', color: '#2F6FDE', uncolored: false, file: '/uishot/starter-base.stl', info: STARTER.base},
        {name: 'Color 2 (#F2B134)', color: '#F2B134', uncolored: false, file: '/uishot/starter-knob.stl', info: STARTER.knob},
    ],
}
const shotModel: ShotModel = starterShot ? starterModel : view === 'plate-overflow' ? plateModel(true) : view === 'viewer-parts' || view === 'print-panel' || view === 'persist' || view.startsWith('projects-') ? plateModel(false) : enclosureModel
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
    {id: 3, workspaceId: 1, providerId: 'ollama', model: 'qwen3-coder:30b', title: es ? 'Caja para Raspberry Pi 5 con tapa deslizante' : 'Raspberry Pi 5 case with a sliding lid', createdAt: 1_790_000_000, updatedAt: 1_790_003_000, cliSession: '', mode: ''},
    {id: 2, workspaceId: 1, providerId: 'ollama', model: 'llama3.2:3b', title: es ? 'Ranuras de ventilación' : 'Ventilation slots', createdAt: 1_789_900_000, updatedAt: 1_789_900_600, cliSession: '', mode: ''},
    // «Solo consulta» elegido a mano para ESTA conversación (S12.c, T12): las otras siguen en «Agente».
    {id: 1, workspaceId: 1, providerId: 'ollama', model: 'qwen3-coder:30b', title: es ? 'Cuántos milímetros de holgura para una tapa' : 'How much clearance for a lid', createdAt: 1_789_800_000, updatedAt: 1_789_800_400, cliSession: '', mode: 'ask'},
]
// ── Servidores de modelos (S12, bloque 12.1) ───────────────────────────────
// Cinco servidores inventados (nada de claves reales: solo `hasToken`). OpenRouter
// es el que recuerda el selector en `chat-models`, para que se vean los precios.
// chat-no-providers: ni CLIs ni servidores (el aviso con el botón «Abrir Proveedores de IA»).
const noProviders = view === 'chat-no-providers'
const serverFixtures = [
    {id: 'ollama', kind: 'ollama', preset: 'ollamaLocal', name: 'MacOS · Ollama', baseUrl: 'http://localhost:11434', apiKeyEnv: '', hasToken: false, defaultModel: 'qwen3-coder:30b', contextTokens: 0, enabled: !noProviders, sortOrder: 5, updatedAt: 1_790_000_000},
    {id: 'srv-4f1a9c2e7b30', kind: 'openai', preset: 'openrouter', name: 'OpenRouter', baseUrl: 'https://openrouter.ai/api/v1', apiKeyEnv: '', hasToken: true, defaultModel: view === 'chat-no-vision' ? 'deepseek/deepseek-chat' : 'anthropic/claude-sonnet-5-5', contextTokens: 0, enabled: !noProviders, sortOrder: 6, updatedAt: 1_790_000_100},
    {id: 'srv-8c3d5e1f0a92', kind: 'openai', preset: 'gemini', name: 'Google Gemini', baseUrl: 'https://generativelanguage.googleapis.com/v1beta/openai', apiKeyEnv: 'GEMINI_API_KEY', hasToken: false, defaultModel: 'gemini-2.5-pro', contextTokens: 0, enabled: !noProviders, sortOrder: 7, updatedAt: 1_790_000_200},
    {id: 'anthropic', kind: 'anthropic', preset: 'anthropic', name: 'Anthropic', baseUrl: 'https://api.anthropic.com', apiKeyEnv: '', hasToken: true, defaultModel: 'claude-sonnet-5-5', contextTokens: 0, enabled: !noProviders, sortOrder: 8, updatedAt: 1_790_000_300},
    {id: 'srv-2b7e6d4a1c58', kind: 'openai', preset: 'lmstudio', name: es ? 'LM Studio del taller' : 'Workshop LM Studio', baseUrl: 'http://192.168.1.40:1234/v1', apiKeyEnv: '', hasToken: false, defaultModel: '', contextTokens: 0, enabled: !noProviders, sortOrder: 9, updatedAt: 1_790_000_400},
]
const mi = (id: string, vision: boolean, tools: boolean, ctx: number, pp = 0, cp = 0) => ({
    id, vision, visionKnown: true, tools, toolsKnown: true, contextTokens: ctx, promptPricePerM: pp, completionPricePerM: cp,
})
const serverModels: Record<string, ReturnType<typeof mi>[]> = {
    ollama: [mi('deepseek-r1:14b', false, false, 131072), mi('gemma3:12b', true, false, 131072), mi('llama3.2:3b', false, true, 131072), mi('qwen3-coder:30b', false, true, 262144), mi('qwen3-vl:8b', true, true, 262144)],
    'srv-4f1a9c2e7b30': [
        mi('anthropic/claude-opus-5-5', true, true, 1_000_000, 15, 75),
        mi('anthropic/claude-sonnet-5-5', true, true, 1_000_000, 3, 15),
        mi('deepseek/deepseek-chat', false, true, 163840, 0.27, 1.1),
        mi('google/gemini-2.5-pro', true, true, 1_048_576, 1.25, 10),
        mi('meta-llama/llama-3.3-70b-instruct', false, true, 131072, 0.13, 0.4),
        mi('mistralai/mistral-small-3.2', true, true, 131072, 0.1, 0.3),
        mi('openai/gpt-5-mini', true, true, 400000, 0.25, 2),
        mi('qwen/qwen3-coder', false, true, 262144, 0.22, 0.95),
    ],
    'srv-8c3d5e1f0a92': [mi('gemini-2.5-flash', true, true, 1_048_576), mi('gemini-2.5-pro', true, true, 1_048_576)],
    anthropic: [mi('claude-opus-5-5', true, true, 1_000_000), mi('claude-sonnet-5-5', true, true, 1_000_000), mi('claude-haiku-5-5', true, true, 200000)],
    'srv-2b7e6d4a1c58': [],
}
// Pruebas de conexión: todo responde bien salvo el LM Studio del taller (apagado).
const probeFor = (id: string) =>
    id === 'srv-2b7e6d4a1c58'
        ? {ok: false, status: 0, message: es ? 'no se pudo conectar a http://192.168.1.40:1234/v1: la conexión fue rechazada (¿está el servidor en marcha?)' : 'could not connect to http://192.168.1.40:1234/v1: connection refused (is the server running?)', models: 0, modelIds: [], latencyMs: 0}
        : {ok: true, status: 200, message: id === 'ollama' ? '0.12.5' : '', models: (serverModels[id] ?? []).length, modelIds: (serverModels[id] ?? []).map((m) => m.id), latencyMs: id === 'ollama' ? 18 : 240}
// ── CLIs como proveedores (S12.b) ──────────────────────────────────────────
// Los cuatro CLIs «instalados» (nada de rutas reales: son inventadas) y lo que el selector sabe de ellos.
const cliFixtures = [
    {id: 'cli:claude', short: 'claude', name: 'Claude Code', vendor: 'Anthropic', version: '2.1.300'},
    {id: 'cli:codex', short: 'codex', name: 'Codex CLI', vendor: 'OpenAI', version: '0.98.0'},
    {id: 'cli:antigravity', short: 'antigravity', name: 'Antigravity CLI', vendor: 'Google', version: '1.7.2'},
    {id: 'cli:opencode', short: 'opencode', name: 'OpenCode', vendor: 'SST', version: '1.4.2'},
]
// chat-providers: el selector con un solo servidor (el del Mac), como en la ayuda.
const providerServers = serverFixtures.filter((x) => x.enabled && (view !== 'chat-providers' || x.id === 'ollama'))
const chatProviderList = noProviders
    ? []
    : [
          ...cliFixtures.map((c) => ({id: c.id, kind: 'cli', name: c.name, vendor: c.vendor, installed: true, path: `/Users/maker/.local/bin/${c.short}`, remote: false, enabled: true})),
          ...providerServers.map((x) => ({id: x.id, kind: x.kind, name: x.name, vendor: '', installed: true, path: '', remote: !/localhost|127\.0\.0\.1/.test(x.baseUrl), enabled: true})),
      ]
const cliModels: Record<string, {id: string; label: string; description: string}[]> = {
    'cli:claude': [
        {id: 'opus', label: 'Opus', description: es ? 'El más capaz, para diseños difíciles' : 'The most capable, for hard designs'},
        {id: 'sonnet', label: 'Sonnet', description: es ? 'Equilibrio entre velocidad y calidad' : 'Balance of speed and quality'},
        {id: 'haiku', label: 'Haiku', description: es ? 'El más rápido, para retoques' : 'The fastest, for tweaks'},
    ],
    'cli:codex': [
        {id: 'gpt-5-codex', label: 'GPT-5 Codex', description: es ? 'Pensado para código' : 'Built for code'},
        {id: 'gpt-5-codex-mini', label: 'GPT-5 Codex Mini', description: es ? 'Más barato y rápido' : 'Cheaper and faster'},
    ],
    'cli:antigravity': [
        {id: 'gemini-4-argon-high', label: 'Gemini 4 Argon (High)', description: es ? 'Razonamiento largo, el más preciso' : 'Long reasoning, the most precise'},
        {id: 'gemini-4-argon-low', label: 'Gemini 4 Argon (Low)', description: es ? 'La misma familia con menos razonamiento' : 'Same family with less reasoning'},
        {id: 'gemini-4-flash', label: 'Gemini 4 Flash', description: es ? 'Respuestas casi instantáneas' : 'Near-instant answers'},
        {id: 'claude-sonnet-5-5-thinking', label: 'Claude Sonnet 5.5 (Thinking)', description: es ? 'Claude con razonamiento extendido' : 'Claude with extended reasoning'},
        {id: 'gpt-oss-240b-medium', label: 'GPT-OSS 240B (Medium)', description: es ? 'Pesos abiertos' : 'Open weights'},
    ],
    'cli:opencode': [
        {id: 'anthropic/claude-sonnet-5-5', label: 'Claude Sonnet 5.5', description: 'anthropic'},
        {id: 'openai/gpt-5-mini', label: 'GPT-5 mini', description: 'openai'},
    ],
}
// Salud: los CLIs responden (instalados, sin latencia); los servidores, con su sondeo; el LM Studio del taller está apagado.
const providerHealth = (id: string) => {
    if (id.startsWith('cli:')) return {ok: true, latencyMs: 0, error: ''}
    const pr = probeFor(id)
    return {ok: pr.ok, latencyMs: pr.latencyMs, error: pr.ok ? '' : pr.message}
}
const chatCatalog = (id: string) => {
    if (id.startsWith('cli:')) {
        return {
            defaultHint: es ? 'El que tenga configurado el CLI' : 'Whatever the CLI has configured',
            models: (cliModels[id] ?? []).map((m) => ({...m, vision: false, visionKnown: false, tools: true, toolsKnown: true, contextTokens: 0, promptPricePerM: 0, completionPricePerM: 0})),
        }
    }
    const def = serverFixtures.find((x) => x.id === id)?.defaultModel
    return {
        defaultHint: def ? (es ? `El predeterminado del servidor (${def})` : `The server's default (${def})`) : es ? 'El primero que devuelva el servidor' : 'The first one the server returns',
        models: (serverModels[id] ?? []).map((m) => ({...m, label: '', description: ''})),
    }
}
// mini-tools (S12.b): lo que vería «Importar servidores» con una base falsa. Dos servidores nuevos y uno que ya está en ScadLab.
const miniToolsPath = platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\mini-tools\\vault.db' : '/Users/maker/Library/Application Support/mini-tools/vault.db'
const miniToolsImport = {
    found: true,
    path: miniToolsPath,
    imported: 0,
    servers: [
        {name: es ? 'Ollama del estudio' : 'Studio Ollama', kind: 'ollama', baseUrl: 'http://192.168.1.60:11434', defaultModel: 'qwen3-coder:30b', exists: false},
        {name: 'Groq', kind: 'openai', baseUrl: 'https://api.groq.com/openai/v1', defaultModel: 'llama-3.3-70b-versatile', exists: false},
        {name: 'OpenRouter', kind: 'openai', baseUrl: 'https://openrouter.ai/api/v1', defaultModel: 'anthropic/claude-sonnet-5-5', exists: true},
    ],
}
// chat-models recuerda a OpenRouter (el que tiene precios); el resto, a Ollama.
const lastModelShot =
    view === 'chat-models'
        ? {serverId: 'srv-4f1a9c2e7b30', model: 'anthropic/claude-sonnet-5-5'}
        : view === 'chat-no-vision'
          ? {serverId: 'srv-4f1a9c2e7b30', model: 'deepseek/deepseek-chat'}
        : view === 'chat-providers' || view === 'chat-empty' || view === 'chat-from-image'
          ? {serverId: 'cli:claude', model: ''}
          : view === 'chat-cli-models'
            ? {serverId: 'cli:antigravity', model: ''}
            : {serverId: 'ollama', model: 'qwen3-coder:30b'}
// vault-create: la contraseña se crea al pulsar «Crear» en el diálogo; el reintento de Guardar ya pasa.
let vaultCreated = false
const serverMocks = {
    ListModelServers: () => P(serverFixtures),
    // vault-create: la primera clave de API aún no tiene contraseña maestra, así que Go responde con el error de
    // código vault-uninitialized (invisible entre dos U+2063) y la interfaz abre «Crear contraseña maestra».
    SaveModelServer: (s: Record<string, unknown>, token?: string) =>
        view === 'vault-create' && token && !vaultCreated
            ? Promise.reject(new Error('\u2063vault-uninitialized\u2063' + (es ? 'primero crea la contraseña maestra que protege las claves de API' : 'first create the master password that protects the API keys')))
            : P({...s, id: s.id || 'srv-new', hasToken: Boolean(s.hasToken) || Boolean(token)}),
    DeleteModelServer: () => P(undefined),
    TestModelServer: (s: {id: string}) => P(probeFor(s.id)),
    ListServerModels: (id: string) => P(serverModels[id] ?? []),
    ListChatProviders: () => P(chatProviderList),
    RefreshChatProviders: () => P(chatProviderList),
    ProviderHealth: (id: string) => P(providerHealth(id)),
    ChatModelCatalog: (id: string) => P(chatCatalog(id)),
    ListDetectedCLIs: () => P(cliFixtures.map((c) => ({id: c.short, name: c.name, vendor: c.vendor, installed: true, path: `/Users/maker/.local/bin/${c.short}`, version: c.version}))),
    ImportMiniToolsServers: (dryRun: boolean) => P(dryRun ? miniToolsImport : {...miniToolsImport, imported: miniToolsImport.servers.filter((x) => !x.exists).length}),
    ListProviderModels: (id: string) => P((serverModels[id] ?? []).map((m) => m.id)),
    GetLastModel: () => P(lastModelShot),
    SetLastModel: () => P(undefined),
    // Llavero propio (12.10): sin contraseña en vault-create; bloqueado y sin recordar en vault-unlock; en el resto,
    // recordado en este equipo (lo normal después del primer uso).
    VaultStatus: () =>
        P({
            initialized: view !== 'vault-create',
            unlocked: view !== 'vault-create' && view !== 'vault-unlock',
            remembered: view !== 'vault-create' && view !== 'vault-unlock',
            keyringAvailable: true,
            keyringDetail: '',
        }),
    CreateMasterPassword: () => ((vaultCreated = true), P({rememberFailed: false, detail: '', lostKeys: 0})),
    UnlockVault: () => P({rememberFailed: false, detail: '', lostKeys: 0}),
    ChangeMasterPassword: () => P({rememberFailed: false, detail: '', lostKeys: 0}),
    ForgetThisDevice: () => P(undefined),
    SetRememberDevice: () => P(undefined),
    ResetVault: () => P(2),
}
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
// ── Hermes (S12, 12.4–12.6): pasos de herramienta, adjuntos y conversaciones ───
// Las imágenes del «preview» y las miniaturas se DIBUJAN aquí con un canvas
// (cajas isométricas sombreadas): el banco no tiene OpenSCAD, y una captura
// real traería el modelo y las carpetas de alguien. Todo es inventado.
const hermesView = view === 'chat' || createsShot || view === 'chat-query-only' || view === 'chat-agent-steps' || view === 'chat-attachments' || view === 'chat-conversations' || view === 'chat-code' || view === 'mesh-ai' || view === 'chat-compare' || view === 'chat-trace' || chatHarness
// El chat del banco corre sobre OpenRouter; el panel angosto (chat-narrow, chat-w*), sobre un CLI: sus pasos usan las mismas tarjetas.
// chat-code y chat-query-only: la conversación abierta está en «Solo consulta», elegido a mano. El modo es de cada
// conversación (chats.mode, S12.c T12): ya no hay nada que fijar en localStorage, y el predeterminado es «Agente».
const hermesChatMode = view === 'chat-code' || view === 'chat-query-only' ? 'ask' : ''
// chat-agent-creates: Claude Code con el MCP de ScadLab; chat-autosave: un modelo local SIN herramientas (Ollama); chat-query-only: Codex en consulta.
const hermesServerId = chatHarness ? 'cli:codex' : view === 'chat-agy-denied' ? 'cli:antigravity' : view === 'chat-agent-creates' ? 'cli:claude' : view === 'chat-autosave' ? 'ollama' : view === 'chat-query-only' ? 'cli:codex' : 'srv-4f1a9c2e7b30'
const hermesModel = chatHarness ? 'gpt-5-codex' : view === 'chat-agy-denied' ? 'gemini-4-argon-high' : view === 'chat-agent-creates' ? 'opus' : view === 'chat-autosave' ? 'llama3.2:3b' : view === 'chat-query-only' ? 'gpt-5-codex' : 'anthropic/claude-sonnet-5-5'

const shadeHex = (hex: string, f: number) => {
    const n = parseInt(hex.slice(1), 16)
    const c = (v: number) => Math.max(0, Math.min(255, Math.round(v * f)))
    return `rgb(${c((n >> 16) & 255)},${c((n >> 8) & 255)},${c(n & 255)})`
}
type Iso = (x: number, y: number, z: number) => [number, number]
function drawScene(w: number, h: number, kind: 'case' | 'bracket' | 'sketch', mime: 'image/png' | 'image/jpeg' = 'image/png'): string {
    const cv = document.createElement('canvas')
    cv.width = w
    cv.height = h
    const c = cv.getContext('2d')!
    const bg = c.createLinearGradient(0, 0, 0, h)
    bg.addColorStop(0, kind === 'sketch' ? '#efe6d2' : '#f4f5f8')
    bg.addColorStop(1, kind === 'sketch' ? '#d9ccb0' : '#dfe2e9')
    c.fillStyle = bg
    c.fillRect(0, 0, w, h)
    const COS = Math.cos(Math.PI / 6)
    const poly = (pts: [number, number][], fill: string) => {
        c.beginPath()
        pts.forEach(([x, y], i) => (i ? c.lineTo(x, y) : c.moveTo(x, y)))
        c.closePath()
        c.fillStyle = fill
        c.fill()
    }
    const cuboid = (iso: Iso, x: number, y: number, z: number, W: number, D: number, H: number, base: string) => {
        poly([iso(x, y + D, z + H), iso(x + W, y + D, z + H), iso(x + W, y + D, z), iso(x, y + D, z)], shadeHex(base, 0.78))
        poly([iso(x + W, y, z + H), iso(x + W, y + D, z + H), iso(x + W, y + D, z), iso(x + W, y, z)], shadeHex(base, 0.58))
        poly([iso(x, y, z + H), iso(x + W, y, z + H), iso(x + W, y + D, z + H), iso(x, y + D, z + H)], shadeHex(base, 1.12))
    }
    if (kind === 'case') {
        // Caja de 85 × 56 × 24 con la cavidad abierta, y su tapa naranja al lado.
        const s = w / 250
        const iso: Iso = (x, y, z) => [w * 0.52 + (x - y) * COS * s, h * 0.2 + (x + y) * 0.5 * s - z * s]
        cuboid(iso, 0, 0, 0, 85, 56, 24, '#2b2f38')
        c.save()
        c.beginPath()
        ;[iso(3, 3, 24), iso(82, 3, 24), iso(82, 53, 24), iso(3, 53, 24)].forEach(([x, y], i) => (i ? c.lineTo(x, y) : c.moveTo(x, y)))
        c.closePath()
        c.clip()
        poly([iso(3, 3, 3), iso(82, 3, 3), iso(82, 53, 3), iso(3, 53, 3)], shadeHex('#2b2f38', 0.55))
        poly([iso(3, 3, 24), iso(3, 53, 24), iso(3, 53, 3), iso(3, 3, 3)], shadeHex('#2b2f38', 0.92))
        poly([iso(3, 3, 24), iso(82, 3, 24), iso(82, 3, 3), iso(3, 3, 3)], shadeHex('#2b2f38', 0.72))
        c.restore()
        cuboid(iso, 6, 70, 0, 85, 56, 3, '#ff7a1a')
    } else if (kind === 'bracket') {
        // Soporte en L con dos agujeros de tornillo.
        const s = w / 150
        const iso: Iso = (x, y, z) => [w * 0.5 + (x - y) * COS * s, h * 0.42 + (x + y) * 0.5 * s - z * s]
        cuboid(iso, 0, 0, 0, 60, 10, 40, '#9aa3b2')
        cuboid(iso, 0, 10, 0, 60, 34, 8, '#9aa3b2')
        for (const x of [14, 46]) poly([iso(x - 4, 22, 8), iso(x + 4, 22, 8), iso(x + 4, 30, 8), iso(x - 4, 30, 8)], '#4a505c')
    } else {
        // «Foto» de un croquis sobre una mesa de madera.
        c.fillStyle = '#b88a5a'
        c.fillRect(0, h * 0.62, w, h * 0.38)
        c.fillStyle = '#fbfaf5'
        c.fillRect(w * 0.18, h * 0.12, w * 0.64, h * 0.62)
        c.strokeStyle = '#3d4350'
        c.lineWidth = Math.max(2, w / 90)
        c.strokeRect(w * 0.28, h * 0.28, w * 0.44, h * 0.3)
        c.beginPath()
        c.moveTo(w * 0.28, h * 0.43)
        c.lineTo(w * 0.72, h * 0.43)
        c.stroke()
    }
    return cv.toDataURL(mime, 0.82)
}
const previewCase = drawScene(480, 320, 'case')
const previewBracket = drawScene(480, 320, 'bracket')
const photoSketch = drawScene(320, 240, 'sketch', 'image/jpeg')

const ra = es
    ? {sentence: 'Voy a crear la caja con paredes de 2 mm y a comprobar que renderiza.', done: 'Listo: dejé el checkpoint **007-caja-rpi5.scad**. Mide 89 × 60 × 24 mm, cabe en la A1 mini y pesa unos 31 g. La tapa deslizante va en un checkpoint aparte, para imprimirla plana. ¿Quieres ranuras de ventilación o recortes para los puertos?'}
    : {sentence: 'I will create the box with 2 mm walls and check that it renders.', done: 'Done: I left the checkpoint **007-rpi5-case.scad**. It measures 89 × 60 × 24 mm, fits the A1 mini and weighs about 31 g. The sliding lid goes in its own checkpoint, so it prints flat. Do you want ventilation slots or port cutouts?'}
const caseFile = es ? '007-caja-rpi5.scad' : '007-rpi5-case.scad'
const hermesStep = (index: number, tool: string, args: Record<string, unknown>, status: 'ok' | 'error', text: string, durationMs: number, textAt: number, images: string[] = []) => ({
    id: `st-${index}`, index, tool, args, status, text, images, durationMs, textAt,
})
const stepsChat = [
    hermesStep(1, 'create_checkpoint', {parent: '006-two-color-logo.scad', label: es ? 'caja rpi5' : 'rpi5 case', description: es ? 'Caja para Raspberry Pi 5' : 'Raspberry Pi 5 case', source: chatScad}, 'ok', `{"file":"${caseFile}","parent":"006-two-color-logo.scad"}`, 38, ra.sentence.length),
    hermesStep(2, 'render', {file: caseFile}, 'ok', `{"file":"${caseFile}","status":"ok","durationMs":1412,"engine":"native","mesh":{"triangles":1836,"watertight":true,"size":[89,60,24]},"fitsBuildVolume":true}`, 1448, ra.sentence.length),
    hermesStep(3, 'preview', {file: caseFile, view: 'iso'}, 'ok', `{"file":"${caseFile}","view":"iso","size":640}`, 2310, ra.sentence.length, [previewCase]),
    hermesStep(4, 'measure', {file: caseFile}, 'ok', `{"file":"${caseFile}","mesh":{"size":[89,60,24],"volumeMm3":18420},"estimate":{"totalGrams":31.4}}`, 96, ra.sentence.length),
]
const hermesUsage = {inputTokens: 18_420, outputTokens: 2_135, costUsd: 0.0873}
const lidFile = es ? '008-tapa-deslizante.scad' : '008-sliding-lid.scad'
const lidFixed = es ? '009-tapa-deslizante-v2.scad' : '009-sliding-lid-v2.scad'
const rb = es
    ? {sentence: 'Creo la tapa deslizante con su riel y la compruebo.', fix: 'Me faltaba cerrar un corchete en la línea 14. Lo corrijo en un checkpoint nuevo y vuelvo a renderizar.', done: `La tapa ya renderiza: **${lidFixed}** mide 85 × 56 × 3 mm y desliza por el riel con 0,3 mm de holgura por lado. En el preview se ve alineada con la caja.`}
    : {sentence: 'I create the sliding lid with its rail and check it.', fix: 'I was missing a closing bracket on line 14. I fix it in a new checkpoint and render again.', done: `The lid renders now: **${lidFixed}** measures 85 × 56 × 3 mm and slides along the rail with 0.3 mm clearance per side. The preview shows it aligned with the case.`}
const renderErr = es
    ? `error: {"file":"${lidFile}","status":"error","durationMs":412,"diagnostics":[{"severity":"error","line":14,"col":24,"message":"syntax error","code":"cube([board_w, 4, 6);"}],"message":"El modelo no renderiza. Corrígelo en un checkpoint NUEVO (create_checkpoint con parent=${lidFile}), nunca editando este."}`
    : `error: {"file":"${lidFile}","status":"error","durationMs":412,"diagnostics":[{"severity":"error","line":14,"col":24,"message":"syntax error","code":"cube([board_w, 4, 6);"}],"message":"The model does not render. Fix it in a NEW checkpoint (create_checkpoint with parent=${lidFile}), never by editing this one."}`
const stepsFix = [
    hermesStep(1, 'create_checkpoint', {parent: caseFile, label: es ? 'tapa deslizante' : 'sliding lid', source: 'color("#ff7a1a") difference() {\n    cube([board_w, board_d, 3]);\n    translate([wall, 0, 1.5]) cube([board_w, 4, 6);\n}\n'}, 'ok', `{"file":"${lidFile}"}`, 41, rb.sentence.length),
    hermesStep(2, 'render', {file: lidFile}, 'error', renderErr, 412, rb.sentence.length),
    hermesStep(3, 'create_checkpoint', {parent: lidFile, label: es ? 'tapa deslizante v2' : 'sliding lid v2', source: 'color("#ff7a1a") difference() {\n    cube([board_w, board_d, 3]);\n    translate([wall, 0, 1.5]) cube([board_w, 4, 6]);\n}\n'}, 'ok', `{"file":"${lidFixed}","parent":"${lidFile}"}`, 37, rb.sentence.length + 2 + rb.fix.length),
    hermesStep(4, 'render', {file: lidFixed}, 'ok', `{"file":"${lidFixed}","status":"ok","durationMs":980,"mesh":{"triangles":412,"watertight":true,"size":[85,56,3]}}`, 1003, rb.sentence.length + 2 + rb.fix.length),
    hermesStep(5, 'preview', {file: lidFixed, view: 'iso'}, 'ok', `{"file":"${lidFixed}","view":"iso","size":640}`, 2144, rb.sentence.length + 2 + rb.fix.length, [previewCase]),
]
const shelfFile = es ? '007-repisa-de-pared.scad' : '007-wall-shelf.scad'
const sentAtt = [{kind: 'image', name: es ? 'croquis-repisa.jpg' : 'shelf-sketch.jpg', mime: 'image/jpeg', size: 187_000, thumb: photoSketch}]
const ro = es
    ? {ask: 'Haz una repisa de pared como la del croquis.', sentence: 'Tomo las medidas del croquis y creo la repisa.', done: `Listo: **${shelfFile}** es una repisa de 150 × 40 × 6 mm con dos tornillos M4. ¿La encajo con el soporte de pared que tienes?`}
    : {ask: 'Make a wall shelf like the one in the sketch.', sentence: 'I take the measurements from the sketch and create the shelf.', done: `Done: **${shelfFile}** is a 150 × 40 × 6 mm shelf with two M4 screws. Shall I fit it to the wall bracket you have?`}
const stepsAtt = [
    hermesStep(1, 'create_checkpoint', {parent: '006-two-color-logo.scad', label: es ? 'repisa de pared' : 'wall shelf', source: chatScad}, 'ok', `{"file":"${shelfFile}"}`, 40, ro.sentence.length),
    hermesStep(2, 'render', {file: shelfFile}, 'ok', `{"file":"${shelfFile}","status":"ok","durationMs":801}`, 830, ro.sentence.length),
]
const hermesChatItem = {id: 3, workspaceId: 1, providerId: hermesServerId, model: hermesModel, cliSession: '', mode: hermesChatMode, title: es ? 'Caja para Raspberry Pi 5 con tapa deslizante' : 'Raspberry Pi 5 case with a sliding lid', createdAt: 1_790_000_000, updatedAt: 1_790_003_000}
const parts = (p: Record<string, unknown>) => JSON.stringify(p)
const codeMd = es
    ? `Te dejo la caja de la **Raspberry Pi 5**. Estas son las medidas que usé:\n\n| Pieza | Medida | Nota |\n|:--|--:|:-:|\n| Placa | 85 × 56 mm | la de la Pi 5 |\n| Paredes | 2 mm | imprimen bien en la A1 mini |\n| Altura | 24 mm | deja sitio al ventilador |\n\n## Qué cambié\n\n- La tapa va en un checkpoint **aparte**, para imprimirla plana.\n- Los tornillos son \`M2.5\`; si quedan justos mira [la guía de tolerancias](https://example.com/tolerancias).\n- [x] Paredes de 2 mm\n- [ ] Ranuras de ventilación\n\n\`\`\`openscad\n${chatScad}\`\`\``
    : `Here is the **Raspberry Pi 5** case. These are the measurements I used:\n\n| Part | Size | Note |\n|:--|--:|:-:|\n| Board | 85 × 56 mm | the Pi 5's |\n| Walls | 2 mm | print well on the A1 mini |\n| Height | 24 mm | leaves room for the fan |\n\n## What I changed\n\n- The lid goes in its **own** checkpoint, so it prints flat.\n- The screws are \`M2.5\`; if they end up tight see [the tolerance guide](https://example.com/tolerances).\n- [x] 2 mm walls\n- [ ] Ventilation slots\n\n\`\`\`openscad\n${chatScad}\`\`\``
// S12.c. El pedido de alguien que no sabe OpenSCAD, y lo que deja cada camino en un workspace SIN checkpoints.
const firstAsk = es ? 'Una caja con tapa a presión para un Raspberry Pi 5' : 'A snap-fit box for a Raspberry Pi 5'
const rc = es
    ? {sentence: 'Voy a crear la primera versión de la caja y a comprobar que renderiza.', done: `Listo: creé el primer checkpoint, **${firstFile}**. Mide 89 × 60 × 24 mm, cabe en tu A1 mini y las paredes son de 2 mm. Se ve en el visor; dime si quieres ajustar la altura o sumar la tapa.`}
    : {sentence: 'I will create the first version of the box and check that it renders.', done: `Done: I created the first checkpoint, **${firstFile}**. It measures 89 × 60 × 24 mm, fits your A1 mini and the walls are 2 mm. You can see it in the viewer; tell me if you want to adjust the height or add the lid.`}
const renderOk = `{"file":"${firstFile}","status":"ok","durationMs":1180,"engine":"native","mesh":{"triangles":${sampleInfo.triangles},"watertight":true,"sizeMm":{"x":89,"y":60,"z":24}},"fitsBuildVolume":true,"printer":{"name":"Bambu Lab A1 mini"}}`
const stepsCreate = [
    hermesStep(1, 'create_checkpoint', {label: es ? 'caja rpi5' : 'rpi5 box', description: es ? 'Caja con tapa a presión para una Raspberry Pi 5' : 'Snap-fit box for a Raspberry Pi 5', source: createdSource}, 'ok', `{"file":"${firstFile}","parent":"","activeParent":"${firstFile}"}`, 41, rc.sentence.length),
    hermesStep(2, 'render', {file: firstFile}, 'ok', renderOk, 1204, rc.sentence.length),
    hermesStep(3, 'preview', {file: firstFile, view: 'iso'}, 'ok', `{"file":"${firstFile}","view":"iso","size":640}`, 2260, rc.sentence.length, [previewCase]),
]
const noToolsNotice = es
    ? '_⚠ Este modelo no admite herramientas: sigo en modo texto. Si escribo el diseño completo en un bloque, ScadLab lo guarda y lo renderiza por mí._'
    : '_⚠ This model does not support tools: continuing in text mode. If I write the complete design in a block, ScadLab saves and renders it for me._'
const autoText = `${noToolsNotice}\n\n${es ? 'Aquí tienes la caja con tapa a presión para tu Raspberry Pi 5:' : 'Here is the snap-fit box for your Raspberry Pi 5:'}\n\n\`\`\`openscad\n${createdSource}\`\`\`\n`
const autoSaved = es ? 'ScadLab guardó el bloque como ' : 'ScadLab saved the block as '
const stepsAuto = [
    hermesStep(1, 'autosave', {label: es ? 'Caja con tapa a presión para una Raspberry Pi 5' : 'Snap-fit box for a Raspberry Pi 5', parent: '', file: firstFile}, 'ok', `{"file":"${firstFile}","parent":"","message":"${autoSaved}${firstFile}"}`, 12, autoText.length),
    hermesStep(2, 'render', {file: firstFile}, 'ok', renderOk, 1204, autoText.length),
]
// chat-agy-denied: lo que arma cliTurnText (app_chat_cli.go) cuando Antigravity bloqueó una herramienta: la nota de «sin
// respuesta» y la explicación de msgCLIAgyDenied (app_agyperms.go) con la herramienta, la ruta y el detalle.
const agySettings = '/Users/maker/.gemini/antigravity-cli/settings.json'
const agyDetail = 'permission check failed for mcp "scadlab/printer_info": user denied permission for mcp(scadlab/printer_info)\nDo not attempt to circumvent this denial.'
const agyDeniedText = es
    ? `_Antigravity CLI terminó el turno sin responder nada._\n\nAntigravity bloqueó la herramienta «printer_info» de ScadLab: en modo no interactivo no puede pedirte permiso y termina el turno. ScadLab la aprueba con una regla \`mcp(scadlab/…)\` en \`permissions.allow\` de ${agySettings}. Usa «Reparar permisos de Antigravity» (en el aviso, o en Configuración → Proveedores de IA) y vuelve a pedirlo. Detalle: ${agyDetail}`
    : `_Antigravity CLI finished the turn without saying anything._\n\nAntigravity blocked ScadLab's “printer_info” tool: in non-interactive mode it cannot ask you for permission and ends the turn. ScadLab approves it with an \`mcp(scadlab/…)\` rule in \`permissions.allow\` of ${agySettings}. Use “Repair Antigravity permissions” (in the notice, or in Settings → AI providers) and ask again. Detail: ${agyDetail}`
const stepsAgyDenied = [hermesStep(1, 'printer_info', {}, 'error', agyDetail, 22, 0)]
const queryText = es
    ? `Te propongo esta caja para la **Raspberry Pi 5**, con paredes de 2 mm:\n\n\`\`\`openscad\n${chatScad}\`\`\`\n\n- Si quieres la tapa a presión, la hago como otro checkpoint.`
    : `Here is a box proposal for the **Raspberry Pi 5**, with 2 mm walls:\n\n\`\`\`openscad\n${chatScad}\`\`\`\n\n- If you want the snap-fit lid, I will make it as another checkpoint.`
// ── S15: modelo desde una imagen ────────────────────────────────────────────────────────────────────────────────────────
// Lo que arma el diálogo (lib/fromImage.ts) y lo que contestan las herramientas de imagen. Los nombres de los campos de los
// resultados JSON son los que lee ToolCallCard (compare_with_image: view, modelAspect, photoAspect, silhouetteIoU;
// trace_image_outline: files, contours, areaMm2, sizeMm).
const frontRef = 'refs/images/escuadra-frente-3f9a1c7e.jpg'
const sideRef = 'refs/images/escuadra-lado-b1042d9a.jpg'
const logoRef = 'refs/images/logo-club-8e5d20c4.png'
const fromImageAsk = (what: string, reference: string, n: number, refs: string[]) =>
    es
        ? `Crea un modelo 3D en OpenSCAD a partir de ${n === 1 ? 'la foto adjunta' : `las ${n} fotos adjuntas`}. Sigue la skill \`openscad-from-image\` (léela con \`read_skill\` antes de empezar).\n- Objeto: ${what}\n- Medida de referencia: ${reference}`
        : `Create a 3D model in OpenSCAD from ${n === 1 ? 'the attached photo' : `the ${n} attached photos`}. Follow the \`openscad-from-image\` skill (read it with \`read_skill\` before you start).\n- Object: ${what}\n- Reference measurement: ${reference}`
const bracketAsk = fromImageAsk(es ? 'Una escuadra en L de metal para sostener un estante' : 'A metal L-bracket to hold up a shelf', es ? 'El ancho total es 60 mm; la moneda de 25 centavos mide 24 mm' : 'The total width is 60 mm; the 25-cent coin is 24 mm across', 2, [frontRef, sideRef])
const bracketAtt = [
    {kind: 'image', name: es ? 'escuadra-frente.jpg' : 'bracket-front.jpg', mime: 'image/jpeg', size: 1_420_000, refPath: frontRef, thumb: '/uishot/photo-bracket-front.png'},
    {kind: 'image', name: es ? 'escuadra-lado.jpg' : 'bracket-side.jpg', mime: 'image/jpeg', size: 960_000, refPath: sideRef, thumb: '/uishot/photo-bracket-side.png'},
]
const bracketFile = es ? '001-escuadra-en-l.scad' : '001-l-bracket.scad'
const rcmp = es
    ? {sentence: 'Leo el método para modelar desde fotos, mido la foto y creo la escuadra con los 60 mm de ancho y la moneda como escala.', done: `Listo: **${bracketFile}** es una escuadra de 60 × 42 × 40 mm con dos agujeros de 8,4 mm. Al compararla con la foto, la silueta coincide al 93 %: la placa vertical me quedó 1,5 mm más alta que la de la foto. ¿La ajusto a 40 mm exactos?`}
    : {sentence: 'I read the method for modeling from photos, measure the photo and create the bracket with the 60 mm width and the coin as scale.', done: `Done: **${bracketFile}** is a 60 × 42 × 40 mm bracket with two 8.4 mm holes. Compared with the photo, the silhouette matches 93%: the vertical plate came out 1.5 mm taller than the one in the photo. Shall I adjust it to exactly 40 mm?`}
const stepsCompare = [
    hermesStep(1, 'read_skill', {name: 'openscad-from-image'}, 'ok', '{"name":"openscad-from-image","source":"builtin"}', 9, rcmp.sentence.length),
    hermesStep(2, 'image_info', {image: frontRef}, 'ok', `{"image":"${frontRef}","width":1600,"height":1200,"aspect":1.333,"format":"jpeg","dominant":["#9d7346","#9aa3b2","#e9e6e0"],"foreground":[0.22,0.14,0.52,0.7]}`, 61, rcmp.sentence.length),
    hermesStep(3, 'create_checkpoint', {label: es ? 'escuadra en L' : 'L-bracket', description: es ? 'Escuadra en L de 60 mm con dos agujeros' : '60 mm L-bracket with two holes', source: chatScad}, 'ok', `{"file":"${bracketFile}","parent":""}`, 39, rcmp.sentence.length),
    hermesStep(4, 'render', {file: bracketFile}, 'ok', `{"file":"${bracketFile}","status":"ok","durationMs":902,"mesh":{"triangles":312,"watertight":true,"sizeMm":{"x":60,"y":42,"z":41.5}},"fitsBuildVolume":true,"printer":{"name":"Bambu Lab A1 mini"}}`, 934, rcmp.sentence.length),
    hermesStep(5, 'compare_with_image', {image: frontRef, file: bracketFile, view: 'auto', mode: 'side'}, 'ok', '{"file":"' + bracketFile + '","view":"iso","mode":"side","modelAspect":1.19,"photoAspect":1.21,"silhouetteIoU":0.93}', 1210, rcmp.sentence.length, ['/uishot/compare-bracket.png']),
]
const logoFile = es ? '001-placa-logo.scad' : '001-logo-plate.scad'
const logoAsk = fromImageAsk(es ? 'La placa con el logo de un club' : 'A plate with a club logo', es ? 'El logo mide 80 mm de ancho' : 'The logo is 80 mm wide', 1, [logoRef])
const logoAtt = [{kind: 'image', name: es ? 'logo-club.png' : 'club-logo.png', mime: 'image/png', size: 612_000, refPath: logoRef, thumb: '/uishot/photo-logo.png'}]
const rtr = es
    ? {sentence: 'Es un logo plano de un solo color: trazo su contorno a 80 mm de ancho y lo extruyo.', done: `Listo: **${logoFile}** extruye el contorno del logo (anillo y estrella calada) 3 mm sobre una base de 0,6 mm. El SVG quedó en \`refs/logo-club.svg\` por si quieres reutilizarlo.`}
    : {sentence: 'It is a flat single-color logo: I trace its outline at 80 mm wide and extrude it.', done: `Done: **${logoFile}** extrudes the logo outline (ring and cut-out star) 3 mm over a 0.6 mm base. The SVG is in \`refs/logo-club.svg\` in case you want to reuse it.`}
const stepsTrace = [
    hermesStep(1, 'read_skill', {name: 'openscad-from-image'}, 'ok', '{"name":"openscad-from-image","source":"builtin"}', 9, rtr.sentence.length),
    hermesStep(2, 'trace_image_outline', {image: logoRef, width_mm: 80, name: 'logo-club'}, 'ok', '{"files":["refs/logo-club.svg"],"contours":3,"areaMm2":2315.6,"sizeMm":[80,80],"layers":1,"snippet":"linear_extrude(3) import(\\"refs/logo-club.svg\\");"}', 184, rtr.sentence.length, ['/uishot/trace-logo.png']),
    hermesStep(3, 'create_checkpoint', {label: es ? 'placa con logo' : 'logo plate', source: chatScad}, 'ok', `{"file":"${logoFile}","parent":""}`, 38, rtr.sentence.length),
    hermesStep(4, 'render', {file: logoFile}, 'ok', `{"file":"${logoFile}","status":"ok","durationMs":1104,"mesh":{"triangles":1480,"watertight":true,"sizeMm":{"x":80,"y":80,"z":3.6}},"fitsBuildVolume":true}`, 1139, rtr.sentence.length),
]
const hermesMessages =
    view === 'chat-compare'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: bracketAsk, parts: parts({attachments: bracketAtt}), inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${rcmp.sentence}\n\n${rcmp.done}`, parts: parts({steps: stepsCompare, model: hermesModel}), inputTokens: 24_310, outputTokens: 1_480, costUsd: 0.0951},
          ]
        : view === 'chat-trace'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: logoAsk, parts: parts({attachments: logoAtt}), inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${rtr.sentence}\n\n${rtr.done}`, parts: parts({steps: stepsTrace, model: hermesModel}), inputTokens: 12_040, outputTokens: 860, costUsd: 0.0487},
          ]
        : view === 'chat-agent-creates'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: firstAsk, parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${rc.sentence}\n\n${rc.done}`, parts: parts({steps: stepsCreate, model: 'claude-opus-5'}), inputTokens: 9_820, outputTokens: 640, costUsd: 0.0612},
          ]
        : view === 'chat-autosave'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: firstAsk, parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: autoText, parts: parts({steps: stepsAuto, model: 'llama3.2:3b'}), inputTokens: 1_210, outputTokens: 380, costUsd: 0},
          ]
        : view === 'chat-agy-denied'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: firstAsk, parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: agyDeniedText, parts: parts({steps: stepsAgyDenied, model: 'gemini-4-argon-high'}), inputTokens: 1_150, outputTokens: 0, costUsd: 0},
          ]
        : view === 'chat-query-only'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: firstAsk, parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: queryText, parts: parts({model: 'gpt-5-codex'}), inputTokens: 4_120, outputTokens: 520, costUsd: 0},
          ]
        : view === 'chat-code'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: es ? 'Haz una caja para una Raspberry Pi 5 con tapa deslizante.' : 'Make a box for a Raspberry Pi 5 with a sliding lid.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: codeMd, parts: parts({model: hermesModel}), inputTokens: 6_420, outputTokens: 910, costUsd: 0.0312},
          ]
        : view === 'chat-agent-steps'
        ? [
              {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: es ? 'Ahora hazme la tapa deslizante, con su riel y 0,3 mm de holgura.' : 'Now make me the sliding lid, with its rail and 0.3 mm clearance.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
              {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${rb.sentence}\n\n${rb.fix}\n\n${rb.done}`, parts: parts({steps: stepsFix, model: hermesModel}), inputTokens: 31_870, outputTokens: 3_940, costUsd: 0.1547},
          ]
        : view === 'chat-attachments'
          ? [
                {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: ro.ask, parts: parts({attachments: sentAtt}), inputTokens: 0, outputTokens: 0, costUsd: 0},
                {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${ro.sentence}\n\n${ro.done}`, parts: parts({steps: stepsAtt, model: hermesModel}), inputTokens: 9_260, outputTokens: 1_020, costUsd: 0.0431},
            ]
          : [
                {id: 1, chatId: 3, role: 'user', createdAt: 1_790_002_900, content: es ? 'Haz una caja para una Raspberry Pi 5 con tapa deslizante.' : 'Make a box for a Raspberry Pi 5 with a sliding lid.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
                {id: 2, chatId: 3, role: 'assistant', createdAt: 1_790_003_000, content: `${ra.sentence}\n\n${ra.done}`, parts: parts({steps: stepsChat, model: hermesModel}), inputTokens: hermesUsage.inputTokens, outputTokens: hermesUsage.outputTokens, costUsd: hermesUsage.costUsd},
            ]
// Lo que dijeron los otros dos chats: la búsqueda de «Conversaciones» también mira el contenido.
const otherChats: Record<number, {id: number; chatId: number; role: string; createdAt: number; content: string; parts: string; inputTokens: number; outputTokens: number; costUsd: number}[]> = {
    2: [
        {id: 21, chatId: 2, role: 'user', createdAt: 1_789_900_100, content: es ? 'Agrega ranuras de ventilación a la caja.' : 'Add ventilation slots to the case.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
        {id: 22, chatId: 2, role: 'assistant', createdAt: 1_789_900_500, content: es ? 'Te propongo seis ranuras de 6 × 14 mm en la pared trasera, a 4 mm del borde de la tapa, para que no se vean al cerrarla.' : 'I suggest six 6 × 14 mm slots on the back wall, 4 mm from the edge of the lid, so they are hidden when it is closed.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
    ],
    1: [
        {id: 11, chatId: 1, role: 'user', createdAt: 1_789_800_100, content: es ? '¿Cuántos milímetros de holgura le doy a una tapa?' : 'How many millimeters of clearance should I give a lid?', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
        {id: 12, chatId: 1, role: 'assistant', createdAt: 1_789_800_300, content: es ? 'Para una A1 mini, 0,3 mm por lado es un buen punto de partida.' : 'For an A1 mini, 0.3 mm per side is a good starting point.', parts: '', inputTokens: 0, outputTokens: 0, costUsd: 0},
    ],
}
const chatsFor = () => (hermesView ? [hermesChatItem, chatItems[1], chatItems[2]] : chatItems)
// Lo que devuelve el diálogo de archivos de «Adjuntar» en chat-attachments.
const pickedAttachments = [
    {id: 'att-1', kind: 'image', name: es ? 'foto-repisa.jpg' : 'shelf-photo.jpg', mime: 'image/jpeg', size: 1_840_000, thumbDataUrl: photoSketch, refPath: '', summary: ''},
    {id: 'att-2', kind: 'model', name: es ? 'soporte-pared.stl' : 'wall-bracket.stl', mime: 'model/stl', size: 412_000, thumbDataUrl: previewBracket, refPath: es ? 'refs/soporte-pared.stl' : 'refs/wall-bracket.stl', summary: (es ? 'Reference: refs/soporte-pared.stl' : 'Reference: refs/wall-bracket.stl') + ' (STL, 3 812 triangles).'},
]
// chat-no-vision: la foto que devuelve «Adjuntar» (una foto inventada de una escuadra, en frontend/public/uishot/).
const noVisionPhoto = {id: 'att-nv', kind: 'image', name: es ? 'escuadra-frente.jpg' : 'bracket-front.jpg', mime: 'image/jpeg', size: 1_420_000, thumbDataUrl: '/uishot/photo-bracket-front.png', refPath: '', summary: ''}
// «Modificar con IA» (mesh-ai): el modelo del checkpoint, ya adjunto, con el informe y la vista previa que arma Go.
const meshAttachment = {id: 'att-ai', kind: 'model', name: es ? 'caja-rpi5.stl' : 'rpi5-case.stl', mime: 'model/stl', size: 412_000, thumbDataUrl: previewCase, refPath: es ? 'refs/caja-rpi5.stl' : 'refs/rpi5-case.stl', summary: 'Reference: refs/rpi5-case.stl (STL, 4 176 triangles).'}
// Registro de lo que la interfaz llamó (window.__calls), para las pruebas con CDP.
const calls = (name: string, args: unknown[]) => {
    const w = window as unknown as {__calls?: [string, unknown[]][]}
    ;(w.__calls ||= []).push([name, args])
}
const chatMocks = {
    ListChats: () => P(['chat-models', 'chat-empty', 'chat-from-image', 'chat-no-vision', 'chat-providers', 'chat-cli-models', 'chat-no-providers'].includes(view) ? [] : chatsFor()),
    GetChat: (id: number) => {
        if (hermesView) {
            return P({chat: id === 3 ? hermesChatItem : (chatItems.find((c) => c.id === id) ?? hermesChatItem), messages: id === 3 ? hermesMessages : (otherChats[id] ?? []), running: false, partial: '', partialSeq: 0, steps: [], usage: {inputTokens: 0, outputTokens: 0, costUsd: 0}})
        }
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
            messages: id === 3 ? chatMessages : (otherChats[id] ?? []),
            running: false, partial: '', partialSeq: 0,
        })
    },
    ChatCheckpointParent: () => P(selectedFile),
    NewChat: () => P(chatItems[0]),
    SendChatMessage: (...a: unknown[]) => (calls('SendChatMessage', a), P(chatMessages[0])),
    CancelChat: (...a: unknown[]) => (calls('CancelChat', a), P(undefined)),
    DeleteChat: (...a: unknown[]) => (calls('DeleteChat', a), P(undefined)),
    RenameChat: (...a: unknown[]) => (calls('RenameChat', a), P(undefined)),
    SetChatMode: (...a: unknown[]) => (calls('SetChatMode', a), P(undefined)),
    SetChatModel: (...a: unknown[]) => (calls('SetChatModel', a), P(undefined)),
    ChooseChatAttachment: () => P(view === 'chat-no-vision' ? [noVisionPhoto] : pickedAttachments),
    ChooseChatImages: () => P(view === 'chat-from-image' ? [noVisionPhoto] : []),
    AddChatAttachmentData: (_ws: number, name: string) => P({...pickedAttachments[1], name}),
    AddChatAttachment: () => P(view === 'mesh-ai' ? meshAttachment : pickedAttachments[1]),
    RemoveChatAttachment: (...a: unknown[]) => (calls('RemoveChatAttachment', a), P(undefined)),
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

// ── S14: slicers, skills y configuración por workspace simulados ────────────
// Los ocho del registro (backend/slicers). Los instalados cambian con la vista solo donde importa; `configured` es la ruta que fijó el usuario.
const slicerApp = (mac: string, win: string) => (platform === 'windows' ? `C:\\Program Files\\${win}` : `/Applications/${mac}`)
const shotSlicers = [
    {id: 'bambu-studio', name: 'Bambu Studio', path: bambuApp, found: true},
    {id: 'orcaslicer', name: 'OrcaSlicer', path: slicerApp('OrcaSlicer.app', 'OrcaSlicer\\orca-slicer.exe'), found: true},
    {id: 'prusaslicer', name: 'PrusaSlicer', path: slicerApp('PrusaSlicer.app', 'Prusa3D\\PrusaSlicer\\prusa-slicer.exe'), found: true},
    {id: 'creality-print', name: 'Creality Print', path: settings.slicerPaths['creality-print'], found: true, configured: settings.slicerPaths['creality-print']},
    {id: 'elegoo-slicer', name: 'ElegooSlicer', path: '', found: false},
    {id: 'anycubic-slicer-next', name: 'Anycubic Slicer Next', path: '', found: false},
    {id: 'snapmaker-orca', name: 'Snapmaker Orca', path: '', found: false},
    {id: 'ultimaker-cura', name: 'UltiMaker Cura', path: '', found: false},
].map((x) => ({configured: '', ...x}))

const brandSkillNames = new Set(BRANDS.map((b) => b.skill))
const brandSkillDesc = (b: (typeof BRANDS)[number], list: string) => (es ? `Perfiles de impresoras ${b.name} (${list}), su multicolor y reglas de diseño FDM.` : `${b.name} printer profiles (${list}), their multicolor setup and FDM design rules.`)
const shotSkills = [
    {name: 'bambu-lab-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[0], 'A1, P1S, X1C, H2D…')},
    {name: 'creality-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[2], 'Ender-3 V3, K1, K2…')},
    {name: 'elegoo-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[3], 'Centauri Carbon, Neptune 4…')},
    {name: 'anycubic-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[4], 'Kobra 3, Kobra S1…')},
    {name: 'prusa-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[1], 'MK4S, CORE One, MINI+…')},
    {name: 'snapmaker-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[5], 'U1, J1s, Artisan…')},
    {name: 'ultimaker-printers', source: 'builtin', dir: '', enabled: true, description: brandSkillDesc(BRANDS[6], 'S3, S5, S7, S8…')},
    {name: 'gear-helper', source: 'user', dir: `${skillsDir}${sep}gear-helper`, enabled: true, description: gearDescription},
    {name: 'hinge-kit', source: 'user', dir: `${skillsDir}${sep}hinge-kit`, enabled: false, description: es ? 'Bisagras de impresión en sitio con holgura de 0,3 mm para la A1 mini.' : 'Print-in-place hinges with 0.3 mm clearance for the A1 mini.'},
    {name: 'openscad-3mf-multicolor', source: 'builtin', dir: '', enabled: true, description: es ? 'Modelos multicolor (AMS, MMU, CFS, ACE…) y exportación 3MF.' : 'Multicolor models (AMS, MMU, CFS, ACE…) and 3MF export.'},
    {name: 'openscad-core', source: 'builtin', dir: '', enabled: true, description: es ? 'Reglas de modelado paramétrico y del workspace.' : 'Parametric modeling and workspace rules.'},
    {name: 'openscad-libraries', source: 'builtin', dir: '', enabled: false, description: es ? 'BOSL2, MCAD y NopSCADlib.' : 'BOSL2, MCAD and NopSCADlib.'},
    {name: 'openscad-templates', source: 'builtin', dir: '', enabled: true, description: es ? 'Diez plantillas paramétricas listas para imprimir.' : 'Ten print-ready parametric templates.'},
].sort((a, b) => a.name.localeCompare(b.name))
// Las de por defecto de una impresora: las encendidas que no son de marca + la de su marca (backend/wsconfig.DefaultSkills).
const defaultSkillsFor = (printerId: string) => {
    const brand = printers.find((p) => p.id === printerId)?.skill
    return shotSkills.filter((x) => x.name === brand || (x.enabled && !brandSkillNames.has(x.name))).map((x) => x.name).sort()
}
// Lo que cada workspace tiene fijado (se llena al guardar): impresora, colores, skills y ajustes de impresión.
type ShotWsConf = {printerId: string; skills: string[] | null; print: Record<string, unknown>}
const wsConf: Record<number, ShotWsConf> = {}
const wsConfFor = (id: number): ShotWsConf => {
    const w = workspaces.find((x) => x.id === id) ?? workspaces[0]
    // El workspace de la captura de configuración tiene lo suyo: cuatro colores en la K1 Max, su skill de marca y un relleno propio.
    wsConf[id] ??= {
        printerId: w.printerId,
        skills: printerWsShot ? ['creality-printers', 'gear-helper', 'openscad-3mf-multicolor', 'openscad-core', 'openscad-templates'] : null,
        print: printerWsShot ? {colors: 4, infillPercent: 20} : {},
    }
    return wsConf[id]
}
const wsConfigView = (id: number) => {
    const w = workspaces.find((x) => x.id === id) ?? workspaces[0]
    const st = wsConfFor(id)
    const base = printers.find((p) => p.id === st.printerId) ?? printers[0]
    const colors = (st.print.colors as number | undefined) || base.default_colors
    const pr = st.print as {material?: string; infillPercent?: number; wallLoops?: number; lineWidthMm?: number; filamentDiameterMm?: number}
    const defaults = defaultSkillsFor(base.id)
    return {
        workspace: {...w, printerId: st.printerId, skills: st.skills, print: st.print},
        printer: {...base, multicolor_slots: colors},
        colors,
        skills: st.skills ?? defaults,
        defaultSkills: defaults,
        print: st.print,
        effective: {
            material: pr.material || settings.defaultMaterial,
            infillPercent: pr.infillPercent ?? settings.infillPercent,
            wallLoops: pr.wallLoops ?? settings.wallLoops,
            lineWidthMm: pr.lineWidthMm ?? settings.lineWidthMm,
            filamentDiameterMm: pr.filamentDiameterMm ?? base.filament_diameter_mm ?? settings.filamentDiameterMm,
        },
    }
}

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

// Una versión guardada del mismo archivo: el cuerpo anterior, pero con el mismo
// `parent=` (update_checkpoint y restaurar conservan el padre de la cabecera).
const versionSource = () => parentSource().replace('parent=002-sliding-lid.scad', 'parent=005-snap-lid.scad')

// Versiones guardadas inventadas de la vista `history-*` (hace 12 minutos, 2 horas y ayer), la más nueva primero.
const historyVersions = () => {
    const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
    return [
        {id: '20261019-221500', savedAt: ago(12), reason: 'ai', tool: 'update_checkpoint', note: es ? 'Logo bicolor para el AMS y caja más ancha' : 'Two-color logo for the AMS and a wider box', size: 1190},
        {id: '20261019-201000', savedAt: ago(130), reason: 'restore', tool: '', note: '', size: 1004},
        {id: '20261018-190500', savedAt: ago(26 * 60), reason: 'ai', tool: 'update_checkpoint', note: es ? 'Tornillos de la tapa a M2.5' : 'Lid screws to M2.5', size: 1042},
    ]
}

const agyPerms = {state: 'ok', path: '/Users/maker/.gemini/antigravity-cli/settings.json', approved: 37, total: 37, stale: 0, managed: true, detail: ''}
const agyStatus = {state: 'missing', agyPath: '/Users/maker/.local/bin/agy', command: `${scadlabBin} --mcp`, current: '', detail: ''}

// ── Editar un STL (S13) ─────────────────────────────────────────────────────
const meshReport = (path: string) => ({
    format: /\.3mf$/i.test(path) ? '3mf' : 'stl',
    triangles: sampleInfo.triangles, min: sampleInfo.min, max: sampleInfo.max, size: sampleInfo.size,
    volumeMm3: sampleInfo.volumeMm3, areaMm2: sampleInfo.areaMm2,
    // La malla de ejemplo es cerrada; la del diálogo de importar es una abierta (el aviso que hay que ver).
    watertight: view !== 'mesh-import',
    parts: [],
})
const meshEditBase = (file: string) => ({
    ref: es ? 'refs/caja-rpi5.stl' : 'refs/rpi5-case.stl',
    parent: file,
    meshPath: `${meshDir}${fakeKey('mesh-edit')}.stl`,
    report: meshReport('x.stl'),
    fromImport: true,
    model: {translate: [-39, -62, 0], rotate: [0, 0, 0], scale: [1, 1, 1], mirror: [false, false, false]},
    color: '',
})
// El código que escribiría Go, resumido: la cabecera, las operaciones y el import. (El real: backend/meshedit.)
const fakeEditCode = (scene: Record<string, unknown>) => {
    const ops = ((scene.ops as string[]) ?? []).map((o, i) => `//   ${i + 1}. ${o}`).join('\n')
    return `// @checkpoint parent=${selectedFile} desc="${es ? 'Edición de STL' : 'STL edit'}"\n// ${es ? 'Operaciones' : 'Operations'}:\n${ops}\n\nmodule modelo() {\n    translate([-39, -62, 0])\n        import("${scene.ref}", convexity = 10);\n}\ncolor("#9ca3af") modelo();\n`
}

const App_ = {
    // Bambu Studio detectado; abrir y mostrar no hacen nada (el banco no lanza apps).
    DetectSlicers: () => P(shotSlicers),
    OpenInSlicer: () => P(undefined),
    RevealPath: () => P(undefined),
    PreviewExportParts: () => P({parts: exportParts, warnings: exportWarnings}),
    ExportCheckpoint: () => P({path: '', format: '3mf', parts: exportParts, warnings: exportWarnings}),
    GetBootstrap: () => P({version: '0.1.0', platform, dataDir: platform === 'windows' ? 'C:\\Users\\maker\\AppData\\Roaming\\ScadLab' : '/Users/maker/Library/Application Support/ScadLab', schemaVersion: 1, initError: '', settings}),
    GetSettings: () => P(settings),
    ListPrinters: () => P(printers),
    ListPrinterBrands: () => P(BRANDS),
    GetWorkspaceConfig: (id: number) => P(wsConfigView(id)),
    DefaultWorkspaceSkills: (printerId: string) => P(defaultSkillsFor(printerId || settings.defaultPrinter)),
    // Guardar cambia la configuración en memoria y emite el evento, como Go (la barra de estado y el panel Impresión lo siguen).
    SaveWorkspaceConfig: (id: number, input: {printerId: string; skills: string[]; print: Record<string, unknown>}) => {
        calls('SaveWorkspaceConfig', [id, input])
        const st = wsConfFor(id)
        st.printerId = input.printerId || st.printerId
        st.skills = input.skills ?? null
        st.print = input.print ?? {}
        const config = wsConfigView(id)
        window.setTimeout(() => emit('workspace:config', config), 0)
        return P({config, removed: [], kept: [], syncError: ''})
    },
    CreateWorkspaceWith: (input: unknown) => (calls('CreateWorkspaceWith', [input]), P(workspaces[0])),
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
    // Editar un STL (S13). PrepareMeshEdit devuelve la malla cruda del ejemplo (enclosure.stl, con su esquina en el origen) y
    // la pose con que se importó: centrada en la placa. Los demás bindings son lo que devolvería Go con datos inventados.
    PrepareMeshEdit: (_id: number, file: string) => P(meshEditBase(file)),
    PreviewMeshEdit: (_id: number, scene: Record<string, unknown>) => P(fakeEditCode(scene)),
    ApplyMeshEdit: (...a: unknown[]) => (calls('ApplyMeshEdit', a), P(es ? '007-edicion-de-stl.scad' : '007-stl-edit.scad')),
    ChooseModelFile: () => P('/Users/maker/Downloads/soporte-abierto.stl'),
    AnalyzeModelFile: (path: string) => P(meshReport(path)),
    ImportModel: (...a: unknown[]) => (calls('ImportModel', a), P({file: es ? '007-soporte-abierto.scad' : '007-open-bracket.scad', ref: 'refs/soporte-abierto.stl', report: meshReport('x.stl'), warnings: []})),
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
    ListTrash: () => P(view === 'trash' || view === 'trash-empty-confirm' ? trashItems : []),
    TrashCheckpoint: () => P(['20261015-120000-001-base-box.scad']),
    MakeCheckpointRoot: () => P(undefined),
    DeleteTrashed: () => P(undefined),
    EmptyTrash: () => P(trashItems.length),
    RestoreCheckpoint: (_id: number, name: string) => P(name.replace(/^\d{8}-\d{6}-/, '')),
    SetActiveParent: () => P(undefined),
    GetActiveParent: () => P(activeParent),
    RenameCheckpoint: () => P(undefined),
    RevealCheckpoint: () => P(undefined),
    ResumeCheckpointSession: () => P({kind: 'terminal', terminalId: 'term-1', agentId: 'claude', chatId: 0}),
    ReadCheckpointPair: (_id: number, file: string) => P({file, parent: '005-snap-lid.scad', parentSource: parentSource(), source}),
    // Historial de versiones (S16): en las vistas `history-*`, el archivo que editó la IA tiene tres versiones guardadas
    // (la más nueva la dejó la IA justo antes de su último cambio); en las demás vistas, ninguna.
    ListCheckpointHistory: (_id: number, file: string) => {
        const versions = historyShot ? historyVersions() : []
        return P({file, versions, editedByAI: versions[0]?.reason === 'ai', max: 20})
    },
    ReadCheckpointVersion: (_id: number, _file: string, versionId: string) =>
        P(versionId === historyVersions()[0].id ? versionSource() : versionSource().replace('width  = 88;', 'width  = 84;')),
    RestoreCheckpointVersion: () => P(historyVersions()[0]),
    ...chatMocks,
    ...serverMocks,
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
    // Permisos de ScadLab en el settings.json de Antigravity (T14): las 37 herramientas aprobadas.
    AntigravityPermissionsStatus: () => P(agyPerms),
    RemoveAntigravityPermissions: () => P(agyPerms),
    RepairAntigravityPermissions: () => P(agyPerms),
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
            {id: 'ollama', kind: 'ollama', name: 'Ollama', command: '', config: {baseUrl: 'http://localhost:11434', model: 'qwen3-coder:30b'}, enabled: !noProviders, sortOrder: 5},
            {id: 'openai', kind: 'openai', name: 'OpenAI-compatible', command: '', config: {baseUrl: 'http://localhost:1234/v1', model: '', apiKeyEnv: 'OPENAI_API_KEY'}, enabled: false, sortOrder: 6},
            {id: 'anthropic', kind: 'anthropic', name: 'Anthropic API', command: '', config: {model: 'claude-sonnet-5-5', apiKeyEnv: 'ANTHROPIC_API_KEY'}, enabled: false, sortOrder: 7},
        ]),
    ListMaterials: () => P(materialList),
    ListSkills: () => P(shotSkills),
    GetSkillsFolder: () => P({path: skillsDir, default: skillsDir, custom: false, exists: true, count: 2}),
    ListSkillFiles: (name: string) =>
        P(
            name === 'gear-helper'
                ? [
                      {path: 'SKILL.md', size: 1180, text: true},
                      {path: 'README.md', size: 240, text: true},
                      {path: 'references/modulos.md', size: 860, text: true},
                      {path: 'templates/engranaje.scad', size: 520, text: true},
                  ]
                : [
                      {path: 'SKILL.md', size: 9800, text: true},
                      {path: 'references/convenciones.md', size: 4200, text: true},
                      {path: 'templates/base.scad', size: 700, text: true},
                  ],
        ),
    ReadSkillFile: (_name: string, path: string) => P(path === 'SKILL.md' ? (_name === 'gear-helper' ? gearSkillMd : coreSkillMd) : path.endsWith('.scad') ? gearScad : '# ' + path + '\n'),
    ValidateSkillMarkdown: (name: string, text: string) => {
        const lines = text.split('\n')
        const i = lines.findIndex((l) => l.startsWith('description:'))
        const diags: {line: number; severity: string; code: string; message: string}[] = []
        if (i >= 0 && lines[i].slice('description:'.length).trim() === '')
            diags.push({line: i + 1, severity: 'error', code: 'SK006', message: es ? 'description está vacía' : 'description is empty'})
        void name
        return P(diags)
    },
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
// ── Chat solo, a un ancho dado (S12.b) ─────────────────────────────────────
// El panel entra con lazy() como en la app (PanelHost): importarlo a secas adelantaba su montaje en las demás vistas y
// cambiaba cuándo el dock termina de repartir el alto (la foto salía con el panel de abajo pisando la barra de pestañas).
const ChatPanel = lazy(() => import('./components/chat/ChatPanel'))
// chat-narrow (360 px) y chat-w320/480/720/1200: el panel del chat sin el resto de la app, de ese ancho en la
// ventana, para ver cómo se pliega su barra y para que uishot-fit.sh compruebe que ni la barra ni la página se
// desbordan. El ancho se fija en el contenedor y no se pide a la ventana: Chrome headless no baja de ~500 px de
// ancho de ventana, así que un panel de 320 px no se puede probar achicando la ventana.
function ChatHarness() {
    return (
        <div style={{width: chatWidth}} className="h-full bg-surface text-fg">
            <Suspense fallback={null}>
                <ChatPanel workspaceId={1} currentFile={selectedFile} onCheckpointCreated={() => {}} />
            </Suspense>
            <ConfirmHost title="ScadLab" />
            <VaultHost />
            <Toaster />
        </div>
    )
}
const MeshEditor = lazy(() => import('./components/meshedit/MeshEditor'))
// mesh-edit-w*: el editor de STL solo, de ese ancho. La sesión se abre antes de montarlo, como hace RenderPanel.
function MeshHarness() {
    return (
        <div style={{width: meshWidth}} className="h-full bg-surface text-fg">
            <Suspense fallback={null}>
                <MeshEditor workspaceId={1} bed={{x: 180, y: 180, z: 180}} swatches={settings.swatches} shot={cfg.shot?.meshEdit} onApplied={() => {}} />
            </Suspense>
            <ConfirmHost title="ScadLab" />
            <Toaster />
        </div>
    )
}
if (chatHarness || meshHarness) {
    setPlatform(platform)
    applyTheme(theme)
    applyAppearance({...APPEARANCE_DEFAULTS, uiScale: scale})
}

setLanguage(lang)
Promise.all([import('./components/viewer/Viewer'), import('./components/meshedit/MeshEditor'), loadRawMesh(`${meshDir}${fakeKey('mesh-edit')}.stl`),  import('./components/terminal/TerminalView'), import('./components/dock/DockWorkbench'), loadCodeEditor(), loadDiffViewer(), loadSkillCodeEditor(), loadMesh(okResult(selectedFile, 0, false).meshPath), loadParts(fakeKey(selectedFile), shotModel.parts.map((_, i) => `/parts/${fakeKey(selectedFile)}/${String(i).padStart(2, '0')}.stl`))])
    .catch(() => {})
    .then(() => (meshHarness ? prepareSession(1, selectedFile) : null))
    .then(() =>
        createRoot(document.getElementById('root')!).render(
            <StrictMode>{chatHarness ? <ChatHarness /> : meshHarness ? <MeshHarness /> : <App shot={cfg.shot} />}</StrictMode>,
        ),
    )

// Para las pruebas con CDP (scripts de revisión locales): el controlador del dock.
;(window as unknown as {__dock: typeof shotTools}).__dock = shotTools
// Idem para la sesión de impresión (prueba de clic sobre una parte).
;(window as unknown as {__print: typeof printStore}).__print = printStore
// Para las pruebas de las pestañas de proyecto con CDP: leer y mover el estado que cada pestaña conserva.
;(window as unknown as {__proj: unknown}).__proj = {editorSession, editorCommands, selectPart}
// Para las pruebas con CDP del chat: emitir un evento de Go y leer lo que llamó la interfaz.
;(window as unknown as {__emit: typeof emit}).__emit = emit

// vh: alto de la ventana pedida (UISHOT… H en uishot.sh). Chrome headless da la página con 87 px menos de alto
// mientras corre y la agranda al fotografiar: el dock se reacomoda en ese último instante y lo que está en
// `position: fixed` y se midió antes (un desplegable abierto) queda unos píxeles fuera de lugar. Con el alto fijado
// desde el principio no hay reacomodo.
const pinH = Number(q.get('vh'))
if (pinH > 0) {
    for (const el of [document.documentElement, document.body, document.getElementById('root')!]) el.style.height = `${pinH}px`
    // Y lo que mide `window.innerHeight` (el espacio que ven los desplegables para acotarse a la ventana) es el alto de
    // la foto, no los 87 px menos de mientras corre.
    Object.defineProperty(window, 'innerHeight', {get: () => pinH, configurable: true})
}

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

// settings-providers: se prueban dos servidores (uno responde, otro no) para que se vean los dos resultados en línea.
// chat-models: se abre el selector de modelos del chat nuevo.
// settings-providers-add: se abre «Agregar servidor» y se elige el atajo de OpenRouter.
if (view === 'settings-providers-add') {
    let tries = 0
    const steps = ['[data-testid="server-add"]', '[data-preset="openrouter"]']
    let step = 0
    const next = window.setInterval(() => {
        const el = document.querySelector<HTMLElement>(steps[step])
        if (el) {
            el.click()
            if (++step === steps.length) {
                window.clearInterval(next)
                // El diálogo se desplaza: el formulario entero queda a la vista.
                window.setTimeout(() => document.querySelector('[data-testid="server-form"]')?.scrollIntoView({block: 'start'}), 150)
            }
        } else if (++tries > 120) window.clearInterval(next)
    }, 150)
}
// Escribe en un <input> controlado de React (el setter nativo + el evento input).
function typeInto(el: HTMLInputElement, value: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', {bubbles: true}))
}

// vault-create: se abre «Agregar servidor», se elige OpenRouter, se pega una clave inventada y se pulsa Guardar:
// Go responde vault-uninitialized y aparece «Crear contraseña maestra», donde se escribe la contraseña dos veces.
// vault-unlock: el chat pide una clave con la llave sin recuperar; se abre «Desbloquear» y se escribe la contraseña.
if (view === 'vault-create' || view === 'vault-unlock') {
    let tries = 0
    let stage = 0
    const run = window.setInterval(() => {
        tries++
        if (tries > 200) window.clearInterval(run)
        if (view === 'vault-unlock') {
            if (stage === 0) {
                if (document.querySelector('[data-testid="chat-switcher"]')) {
                    stage = 1
                    void import('./lib/vault').then((m) => void m.requestVault('unlock'))
                }
            } else {
                const f = document.querySelector<HTMLFormElement>('[data-testid="vault-unlock"]')
                const input = f?.querySelector<HTMLInputElement>('input[type="password"]')
                if (input) {
                    typeInto(input, 'Bosque-azul-42')
                    window.clearInterval(run)
                }
            }
            return
        }
        const click = (sel: string) => document.querySelector<HTMLElement>(sel)?.click()
        const form = document.querySelector<HTMLFormElement>('[data-testid="vault-create"]')
        if (form) {
            const [pw, confirm] = Array.from(form.querySelectorAll<HTMLInputElement>('input[type="password"]'))
            if (pw && confirm) {
                typeInto(pw, 'Bosque-azul-42')
                typeInto(confirm, 'Bosque-azul-42')
                window.clearInterval(run)
            }
        } else if (stage === 0 && document.querySelector('[data-testid="server-add"]')) {
            click('[data-testid="server-add"]')
            stage = 1
        } else if (stage === 1 && document.querySelector('[data-preset="openrouter"]')) {
            click('[data-preset="openrouter"]')
            stage = 2
        } else if (stage === 2) {
            const key = document.querySelector<HTMLInputElement>('[data-testid="server-form"] input[type="password"]')
            if (key) {
                typeInto(key, 'sk-or-v1-clave-inventada-0000')
                stage = 3
            }
        } else if (stage === 3) {
            // Guardar: el último botón primario del formulario.
            const save = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-testid="server-form"] button')).find((b) => /^(Guardar|Save)$/.test(b.textContent?.trim() ?? ''))
            if (save && !save.disabled) {
                save.click()
                stage = 4
            }
        }
    }, 150)
}

// chat-providers: se abre el selector de proveedor; chat-cli-models: el de modelos de un CLI;
// settings-import-minitools: el botón «Importar servidores de mini-tools» (abre la vista previa).
const clickParam = q.get('click')
const CLICK_ONE: Record<string, string> = {
    'chat-providers': '[data-testid="provider-picker"]',
    'chat-cli-models': '[data-testid="model-picker"]',
    'settings-import-minitools': '[data-testid="import-minitools"]',
}
if (clickParam || CLICK_ONE[view]) {
    let tries = 0
    const click = window.setInterval(() => {
        const btn = document.querySelector<HTMLElement>(clickParam || CLICK_ONE[view])
        if (btn && !btn.hasAttribute('disabled')) {
            // Un respiro: el selector se abre cuando ya llegaron la lista de proveedores y la de modelos y el dock
            // terminó de acomodarse (un desplegable abierto antes queda unos píxeles más arriba que la barra).
            window.setTimeout(() => btn.click(), Math.max(1200, 5000 - performance.now()))
            window.clearInterval(click)
        } else if (++tries > 100) window.clearInterval(click)
    }, 100)
}

if (view === 'settings-providers' || view === 'chat-models') {
    const sel = view === 'settings-providers' ? ['ollama', 'srv-2b7e6d4a1c58'].map((id) => `[data-server="${id}"] [data-testid="server-test"]`) : ['[data-testid="model-picker"]']
    let tries = 0
    const click = window.setInterval(() => {
        const btns = sel.map((q) => document.querySelector<HTMLElement>(q))
        if (btns.every(Boolean)) {
            btns.forEach((b) => b!.click())
            window.clearInterval(click)
        } else if (++tries > 80) window.clearInterval(click)
    }, 100)
}

// chat-agy-denied: el aviso que emite la app cuando Antigravity bloquea una herramienta (App.tsx), con su botón.
if (view === 'chat-agy-denied') {
    window.setTimeout(() => {
        const t = dict()
        toast.withActions(
            fmt(t.chat.antigravityDenied, {tool: 'printer_info', path: agySettings}),
            [{label: t.chat.agyRepair, title: t.chat.agyRepairHint, run: () => {}}],
            t.export.dismiss,
            'agy-denied',
            'error',
        )
    }, 1500)
}

// chat-attachments: se elige «Adjuntar» (el diálogo simulado devuelve una foto y un STL) y se escribe el mensaje.
// chat-conversations: se abre la lista de conversaciones y la segunda pasa a renombrado en línea.
if (view === 'chat-attachments' || view === 'chat-conversations' || view === 'chat-no-vision') {
    let tries = 0
    let stage = 0
    const run = window.setInterval(() => {
        tries++
        // El dock termina de repartir el alto un instante después de montar: un desplegable abierto antes se
        // posiciona sobre una barra que todavía no está en su sitio definitivo.
        if (performance.now() < 5000) return
        if (view === 'chat-attachments' || view === 'chat-no-vision') {
            if (stage === 0) {
                const btn = document.querySelector<HTMLElement>('[data-testid="chat-attach"]')
                if (btn && !btn.hasAttribute('disabled')) {
                    btn.click()
                    stage = 1
                }
            } else if (document.querySelectorAll('[data-attach-chip]').length >= (view === 'chat-no-vision' ? 1 : 2)) {
                const ta = document.querySelector<HTMLTextAreaElement>('[data-font-area="chat"] textarea')
                if (ta) {
                    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!
                    set.call(ta, view === 'chat-no-vision' ? (es ? 'Haz una escuadra como la de la foto.' : 'Make a bracket like the one in the photo.') : es ? 'Ahora encaja la repisa con este soporte de pared y mira la foto de cómo queda.' : 'Now fit the shelf to this wall bracket and look at the photo of how it ends up.')
                    ta.dispatchEvent(new Event('input', {bubbles: true}))
                    window.clearInterval(run)
                }
            }
        } else if (stage === 0) {
            const sw = document.querySelector<HTMLElement>('[data-testid="chat-switcher"]')
            if (sw) {
                sw.click()
                stage = 1
            }
        } else if (stage === 1) {
            // La búsqueda mira título y contenido: «tapa» está en dos títulos y en lo que dijo un tercer chat.
            const box = document.querySelector<HTMLInputElement>('[data-testid="chat-search"]')
            if (box) {
                typeInto(box, es ? 'tapa' : 'lid')
                stage = 2
            }
        } else if (document.querySelectorAll('[data-conversations] [role="option"]').length >= 3) {
            const rename = document.querySelectorAll<HTMLElement>('[data-testid="chat-rename"]')[1]
            if (rename) {
                rename.click()
                window.clearInterval(run)
            }
        }
        if (tries > 160) window.clearInterval(run)
    }, 100)
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
// chat-w*: con poke=1 se abren, uno tras otro, el selector de proveedor, el de modelo, el del modo, el menú «…» y la lista
// de conversaciones, y se anota dónde quedó cada desplegable: uishot-fit.sh falla si alguno se sale de la ventana.
const popRects: Record<string, number[] | null> = {}
if (chatHarness && poke) {
    ;(window as unknown as {__popRects: typeof popRects}).__popRects = popRects
    const ids = ['provider-picker', 'model-picker', 'mode-control', 'chat-more', 'chat-switcher']
    let k = 0
    const next = () => {
        if (k >= ids.length) {
            popRects.done = [1]
            return
        }
        const id = ids[k++]
        const btn = document.querySelector<HTMLElement>(`[data-testid="${id}"]`)
        if (!btn) {
            popRects[id] = null // este ancho no lo muestra (el modo pasa al menú «…» en un panel muy angosto)
            next()
            return
        }
        btn.click()
        window.setTimeout(() => {
            const pop = [...document.body.children].find((el) => (el.matches('[role="dialog"]:not(dialog)') || el.matches('[role="menu"]')) && getComputedStyle(el).visibility === 'visible')
            const r = pop?.getBoundingClientRect()
            popRects[id] = r ? [Math.round(r.left), Math.round(r.top), Math.round(r.right), Math.round(r.bottom)] : [-1, -1, -1, -1]
            document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))
            window.setTimeout(next, 120)
        }, 350)
    }
    window.setTimeout(next, 900)
}
// Los contenedores con desplazamiento vertical de la barra lateral (las dos listas), elegidos por su
// estilo y no por data-lock: el chequeo no puede depender de la marca que debe comprobar.
const sideLists = () =>
    meshHarness ? [] : [...(document.querySelector('aside')?.children ?? [])].filter((c): c is HTMLElement => c instanceof HTMLElement && ['auto', 'scroll', 'hidden'].includes(getComputedStyle(c).overflowY) && c.tagName === 'DIV' && c.scrollHeight > 0 && !c.className.includes('h-px'))
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
    // La barra de estado, no el pie de un diálogo abierto (el de «Crear desde una imagen» vive dentro del chat, antes en el DOM).
    const bar = [...document.querySelectorAll('footer')].find((f) => !f.closest('dialog'))?.getBoundingClientRect()
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
            const r = meshHarness ? undefined : document.querySelector('aside span')?.getBoundingClientRect()
            return r ? Math.round(r.left) : null
        })(),
        toggleLeft: (() => {
            const r = document.querySelector('header button')?.getBoundingClientRect()
            return r ? Math.round(r.left) : null
        })(),
        platform,
        // Chat (S12.b): la barra superior es de una sola línea y no puede tener contenido más ancho que su caja; tampoco
        // la lista de mensajes ni el panel entero. `popRects` es dónde quedó cada desplegable (solo con poke=1).
        // Un chat minimizado o aparcado (ancho 0 o la franja de un grupo minimizado) no se dibuja: no se mide.
        chatBoxes: [...document.querySelectorAll<HTMLElement>('[data-chat-header], [data-chat-scroll], [data-font-area="chat"]')]
            .filter(() => (document.querySelector<HTMLElement>('[data-font-area="chat"]')?.clientWidth ?? 0) >= 100)
            .map((el) => ({
                name: el.dataset.chatHeader !== undefined ? 'barra' : el.dataset.chatScroll !== undefined ? 'mensajes' : 'panel',
                sw: el.scrollWidth,
                cw: el.clientWidth,
            })),
        // Editor de STL (S13): la barra, el panel lateral, el pie y el editor entero no pueden tener contenido más ancho que su caja.
        meshBoxes: [...document.querySelectorAll<HTMLElement>('[data-meshedit-toolbar], [data-meshedit-footer], [data-meshedit-side], [data-meshedit-root]')].map((el) => ({
            name: el.dataset.meshedit !== undefined ? 'editor' : el.hasAttribute('data-meshedit-toolbar') ? 'barra' : el.hasAttribute('data-meshedit-footer') ? 'pie' : el.hasAttribute('data-meshedit-side') ? 'panel' : 'editor',
            sw: el.scrollWidth,
            cw: el.clientWidth,
        })),
        popRects: chatHarness && poke ? popRects : null,
        footerBottom: bar ? Math.round(bar.bottom) : null,
        footerRight: bar ? Math.round(bar.right) : null,
    })
}
window.setInterval(report, 200)
new MutationObserver(report).observe(document.body, {childList: true, subtree: true})
