#!/usr/bin/env node
// «Cerrar y abrir» de la interfaz, por CDP, sobre el banco de capturas.
//
// El banco guarda en localStorage lo que Go guardaría en scadlab.db (parámetro
// persist=reset|1 de frontend/src/uishot.tsx). Cada escenario:
//   1. abre el banco con persist=reset (parte de cero),
//   2. cambia lo visual con interacciones reales (clics, atajos, la API del dock),
//   3. espera a que venzan las esperas de guardado (dock 600 ms, visor 600 ms, checkpoint 400 ms),
//   4. RECARGA la página con persist=1 (un arranque en frío de todo el JavaScript),
//   5. comprueba que cada cosa volvió como se dejó.
//
// Uso: node scripts/uishot-persist.mjs <puerto CDP> <puerto vite>   (lo llama uishot-persist.sh)
const [CDP, VITE] = process.argv.slice(2, 4).map(Number)
const BASE = `http://localhost:${VITE}/uishot.html?view=persist&lang=es`
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// ── Cliente CDP mínimo ──────────────────────────────────────────────────────
const targets = await (await fetch(`http://127.0.0.1:${CDP}/json/list`)).json()
const page = targets.find((t) => t.type === 'page')
const ws = new WebSocket(page.webSocketDebuggerUrl)
await new Promise((res, rej) => {
    ws.onopen = res
    ws.onerror = rej
})
let seq = 0
const waiting = new Map()
ws.onmessage = (m) => {
    const d = JSON.parse(m.data)
    if (d.id && waiting.has(d.id)) {
        waiting.get(d.id)(d)
        waiting.delete(d.id)
    }
}
const send = (method, params = {}) =>
    new Promise((res, rej) => {
        const id = ++seq
        waiting.set(id, (d) => (d.error ? rej(new Error(`${method}: ${d.error.message}`)) : res(d.result)))
        ws.send(JSON.stringify({id, method, params}))
    })
// evaluate corre una expresión en la página y devuelve su valor (JSON).
const evaluate = async (expr) => {
    const r = await send('Runtime.evaluate', {expression: expr, returnByValue: true, awaitPromise: true})
    if (r.exceptionDetails) throw new Error(`${expr.slice(0, 80)}… → ${r.exceptionDetails.exception?.description ?? r.exceptionDetails.text}`)
    return r.result.value
}
await send('Page.enable')
await send('Emulation.setDeviceMetricsOverride', {width: 1440, height: 900, deviceScaleFactor: 1, mobile: false})

async function open(persist) {
    await send('Page.navigate', {url: `${BASE}&persist=${persist}`})
    for (let i = 0; i < 120; i++) {
        await sleep(250)
        const ready = await evaluate(
            `!!(window.__dock && window.__dock.getApi() && document.querySelector('header') && document.querySelector('button[title^="Pintar la pieza de"]'))`,
        ).catch(() => false)
        if (ready) {
            await sleep(600) // que terminen de asentarse el dock y las restricciones
            return
        }
    }
    throw new Error('la app no arrancó')
}

// ── Aserciones ──────────────────────────────────────────────────────────────
let failed = 0
function check(name, got, want) {
    const ok = JSON.stringify(got) === JSON.stringify(want)
    if (!ok) failed++
    console.log(`${ok ? 'OK    ' : 'FALLA '} ${name}${ok ? '' : `  → recibió ${JSON.stringify(got)}, esperaba ${JSON.stringify(want)}`}`)
}

// Lo visible, leído del DOM y del dock: lo mismo se lee antes de cerrar y tras reabrir.
const READ = `(() => {
    const press = (re) => [...document.querySelectorAll('button[aria-pressed]')].find((b) => re.test(b.title))?.getAttribute('aria-pressed')
    const api = window.__dock.getApi()
    const swatches = [...document.querySelectorAll('button[title^="Pintar la pieza de"]')]
    const group = (id) => api.getPanel(id)?.group
    return {
        selected: document.querySelector('[role="treeitem"][aria-selected="true"]')?.dataset.file ?? null,
        wireframe: press(/alambre|sólido/), edges: press(/aristas/), ortho: press(/ortográfica|perspectiva/),
        split: press(/vista dividida/i),
        swatch: swatches.findIndex((b) => b.getAttribute('aria-pressed') === 'true'),
        modelColors: [...document.querySelectorAll('button[aria-pressed]')].find((b) => /Pintar cada parte/.test(b.title))?.getAttribute('aria-pressed'),
        panes: document.querySelectorAll('canvas').length,
        sidebar: !!document.querySelector('aside'),
        statusBar: !!document.querySelector('footer'),
        panels: api.panels.map((p) => p.id).sort(),
        activeOfBottom: group('agent-1')?.activePanel?.id,
        activeOfRight: group('source')?.activePanel?.id,
        maximized: api.hasMaximizedGroup(),
        minimizedSource: group('source')?.element.dataset.min ?? null,
        zen: !document.querySelector('aside') && !document.querySelector('footer') && api.panels.length === 1,
    }
})()`

const saved = () => evaluate(`JSON.parse(localStorage.getItem('uishot.persist.v1') ?? '{}')`)
const keydown = (init) => evaluate(`window.dispatchEvent(new KeyboardEvent('keydown', Object.assign({bubbles: true, cancelable: true}, ${JSON.stringify(init)})))`)
const clickTitle = (re) => evaluate(`(() => { const b = [...document.querySelectorAll('button')].find((x) => ${re}.test(x.title)); b?.click(); return !!b })()`)

// ── Escenario 1: lo del workspace y lo visual ───────────────────────────────
console.log('\n== 1. Checkpoint elegido, visor, paneles, barras ==')
await open('reset')
const before = await evaluate(READ)
check('arranque: un checkpoint distinto del que se va a elegir', before.selected === '003-vent-slots.scad', false)
await evaluate(`document.querySelector('[role="treeitem"][data-file="003-vent-slots.scad"]').click()`)
await sleep(300)
// Visor: alambre, aristas, ortográfica, vista dividida, muestra 4 (y por eso colores del modelo apagados).
for (const re of [/alambre/, /aristas/, /ortográfica/, /vista dividida/i]) check(`clic en ${re}`, await clickTitle(re), true)
await evaluate(`document.querySelectorAll('button[title^="Pintar la pieza de"]')[3].click()`)
// Dock: la pestaña Problemas del grupo de abajo, el código minimizado y Chat como pestaña activa a la derecha.
await evaluate(`window.__dock.getApi().getPanel('problems').api.setActive()`)
await evaluate(`window.__dock.minimize('source')`)
await sleep(1800)
const live = await evaluate(READ)
const store = await saved()
console.log('   guardado:', JSON.stringify({keys: Object.keys(store.settings ?? {}), last: store.last, viewer: store.settings?.viewer}))
check('antes de cerrar: checkpoint 003', live.selected, '003-vent-slots.scad')

await open('1') // ← «abrir otra vez»
const after = await evaluate(READ)
check('checkpoint elegido de vuelta (no el más nuevo)', after.selected, '003-vent-slots.scad')
check('alambre', after.wireframe, 'true')
check('aristas', after.edges, 'true')
check('ortográfica', after.ortho, 'true')
check('vista dividida abierta', after.split, 'true')
check('muestra elegida (4.ª)', after.swatch, 3)
check('modelo con la muestra (no con sus colores)', after.modelColors, 'false')
check('pestaña activa del grupo de abajo: Problemas', after.activeOfBottom, 'problems')
check('el código sigue minimizado (a una franja)', after.minimizedSource, 'strip')
check('están todos los paneles', after.panels, live.panels)

// ── Escenario 1b: las barras ────────────────────────────────────────────────
console.log('\n== 1b. Barra lateral y barra de estado ocultas ==')
await open('reset')
await keydown({code: 'KeyB', key: 'b', metaKey: true})
check('menú Ver abre', await evaluate(`(() => { const b = [...document.querySelectorAll('header button')].find((x) => x.textContent.trim() === 'Ver'); b?.click(); return !!b })()`), true)
await sleep(300)
check('«Ocultar la barra de estado»', await evaluate(`(() => { const i = [...document.querySelectorAll('[role="menu"] [role="menuitemcheckbox"]')].find((x) => /Ocultar la barra de estado/.test(x.title)); i?.click(); return !!i })()`), true)
await sleep(1200)
const hidden = await evaluate(READ)
check('antes de cerrar: sin barra lateral ni de estado', [hidden.sidebar, hidden.statusBar], [false, false])
await open('1')
const bars = await evaluate(READ)
check('barra lateral oculta de vuelta', bars.sidebar, false)
check('barra de estado oculta de vuelta', bars.statusBar, false)

// ── Escenario 2: panel maximizado ───────────────────────────────────────────
console.log('\n== 2. Panel maximizado ==')
await open('reset')
await evaluate(`(() => { const a = window.__dock.getApi(); a.maximizeGroup(a.getPanel('viewport')) })()`)
await sleep(1500)
check('maximizado antes de cerrar', (await evaluate(READ)).maximized, true)
check('el diseño guardado lleva maximizedNode', (await evaluate(`localStorage.getItem('uishot.persist.v1')`)).includes('maximizedNode'), true)
await open('1')
check('maximizado de vuelta', (await evaluate(READ)).maximized, true)

// ── Escenario 3: el modo enfoque NO persiste ────────────────────────────────
console.log('\n== 3. Modo enfoque (no debe persistir) ==')
await open('reset')
await evaluate(`window.__dock.getApi().getPanel('problems').api.setActive()`)
const full = await evaluate(READ)
// Justo después del cambio, antes de que venza la espera de guardado (600 ms): el modo enfoque
// cancela esa espera, y sin guardar antes el diseño de antes se perdería.
await keydown({code: 'Digit0', key: ')', metaKey: true, shiftKey: true})
await sleep(1500)
check('en modo enfoque: un solo panel y sin barras', (await evaluate(READ)).zen, true)
await open('1') // se cierra la app EN modo enfoque
const reopened = await evaluate(READ)
check('al reabrir no está en modo enfoque', reopened.zen, false)
check('al reabrir están todos los paneles', reopened.panels, full.panels)
check('al reabrir: barra lateral y de estado visibles', [reopened.sidebar, reopened.statusBar], [true, true])
check('al reabrir: la pestaña activa de antes del modo enfoque', reopened.activeOfBottom, 'problems')

ws.close()
console.log(failed ? `\n${failed} comprobación(es) fallida(s)` : '\nTodo volvió como se dejó.')
process.exit(failed ? 1 : 0)
