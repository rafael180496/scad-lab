#!/usr/bin/env node
// Pruebas de lo puro del pase de UX del chat (S12.b, 12.13): el contador de contexto, el orden de los
// proveedores, el Markdown (tablas con alineación, casillas, tachado, direcciones sueltas), la búsqueda en las
// conversaciones y los escalones de ancho de la barra superior.
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {compactTokens, contextUsage, estimateTokens} from '../src/lib/chatContext.ts'
import {isCLI, sortProviders} from '../src/lib/chatProviders.ts'
import {snippetAround} from '../src/lib/chatSearch.ts'
import {TIER_LIMITS, tierFor} from '../src/lib/chatTier.ts'
import {parseInline, parseMarkdown} from '../src/components/chat/markdownParse.ts'
import {asksToCreate} from '../src/lib/chatIntent.ts'
import {CREATING_TOOLS, createdCheckpoint} from '../src/lib/chatSteps.ts'
import {readFileSync} from 'node:fs'
import es from '../src/i18n/es.ts'
import en from '../src/i18n/en.ts'
import {fromImageMessage} from '../src/lib/fromImage.ts'

import {LEGACY_MODE_KEYS, chatModeValue, clearLegacyChatMode, isAgentMode} from '../src/lib/chatMode.ts'

// localStorage de mentira para probar la limpieza de las preferencias de modo de versiones anteriores.
const store = new Map()
globalThis.localStorage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => void store.set(k, String(v)),
    removeItem: (k) => void store.delete(k),
}

// ── Contexto: 4 caracteres ≈ 1 token ──
assert.equal(estimateTokens(''), 0)
assert.equal(estimateTokens('hola'), 1)
assert.equal(estimateTokens('hola!'), 2, 'se redondea hacia arriba')
assert.equal(estimateTokens('a'.repeat(400)), 100)
assert.equal(estimateTokens('😀😀😀😀'), 1, 'un emoji pesa como un carácter, no como dos unidades UTF-16')

const u = contextUsage(60, 'a'.repeat(40), 100)
assert.equal(u.used, 70)
assert.equal(u.level, 'warn', '70 % de la ventana ya avisa')
assert.equal(contextUsage(50, '', 100).level, 'ok')
assert.equal(contextUsage(90, '', 100).level, 'full')
assert.equal(contextUsage(500, '', 0).level, 'ok', 'sin ventana conocida no hay aviso')
assert.equal(contextUsage(500, '', 0).ratio, 0)
assert.equal(compactTokens(999, 'en-US'), '999')
assert.equal(compactTokens(1500, 'en-US'), '1.5k')
assert.equal(compactTokens(262_144, 'en-US'), '262k')
assert.equal(compactTokens(1_000_000, 'en-US'), '1M')

// ── Proveedores: los CLIs primero, sin reordenar dentro de cada grupo ──
assert.ok(isCLI('cli:claude'))
assert.ok(!isCLI('srv-4f1a'))
const mixed = [{id: 'ollama'}, {id: 'cli:codex'}, {id: 'srv-1'}, {id: 'cli:claude'}]
assert.deepEqual(sortProviders(mixed).map((p) => p.id), ['cli:codex', 'cli:claude', 'ollama', 'srv-1'])

// ── Escalones de ancho de la barra ──
assert.equal(tierFor(320), 'tiny')
assert.equal(tierFor(TIER_LIMITS.narrow - 1), 'tiny')
assert.equal(tierFor(TIER_LIMITS.narrow), 'narrow')
assert.equal(tierFor(TIER_LIMITS.mid - 1), 'narrow')
assert.equal(tierFor(TIER_LIMITS.mid), 'mid')
assert.equal(tierFor(TIER_LIMITS.wide), 'wide')
assert.equal(tierFor(1200), 'wide')
assert.equal(tierFor(Infinity), 'wide', 'antes de medir, el panel se pinta completo')

// ── Búsqueda: fragmento alrededor de la coincidencia ──
assert.equal(snippetAround('hola mundo', 'MUNDO'), 'hola mundo', 'sin distinguir mayúsculas')
assert.equal(snippetAround('hola mundo', 'zzz'), '')
assert.equal(snippetAround('hola mundo', '  '), '', 'una búsqueda en blanco no coincide con todo')
const long = `${'a '.repeat(60)}tapa${' b'.repeat(60)}`
const sn = snippetAround(long, 'tapa')
assert.ok(sn.startsWith('…') && sn.endsWith('…') && sn.includes('tapa') && sn.length < 120, sn)
assert.ok(!snippetAround('línea uno\n\n   línea   dos tapa', 'tapa').includes('\n'), 'una sola línea')

// ── Markdown ──
const md = parseMarkdown('| Pieza | Medida | Nota |\n|:--|--:|:-:|\n| Placa | 85 mm | ok |\n')
assert.equal(md.length, 1)
assert.equal(md[0].type, 'table')
assert.deepEqual(md[0].align, ['left', 'right', 'center'], 'la alineación sale de la fila de guiones (también con un solo guion)')
assert.deepEqual(md[0].rows, [['Placa', '85 mm', 'ok']])

const list = parseMarkdown('- [x] hecho\n- [ ] pendiente\n- normal\n')[0]
assert.equal(list.type, 'list')
assert.deepEqual(list.items.map((i) => i.task), ['done', 'open', undefined])
assert.equal(list.items[0].text, 'hecho', 'la casilla no queda en el texto')

const inl = parseInline('mira ~~esto~~ y https://example.com/a_b.')
assert.deepEqual(inl.map((n) => n.type), ['text', 'del', 'text', 'link', 'text'])
assert.equal(inl[3].href, 'https://example.com/a_b', 'el punto final de la frase no es parte de la dirección')
const linked = parseInline('[https://x.test](https://y.test)')
assert.equal(linked.length, 1)
assert.equal(linked[0].type, 'link')
assert.deepEqual(linked[0].children.map((c) => c.type), ['text'], 'un enlace no lleva otro enlace adentro')
assert.equal(parseInline('javascript:alert(1)').some((n) => n.type === 'link'), false, 'solo http(s) se convierte en enlace')

// ── Modo del chat (S12.c, T12): es de cada conversación, «Agente» por defecto ──
assert.equal(isAgentMode(''), true, 'una conversación nueva (modo vacío) es Agente')
assert.equal(isAgentMode(undefined), true, 'sin dato, Agente')
assert.equal(isAgentMode(null), true)
assert.equal(isAgentMode('agent'), true)
assert.equal(isAgentMode('ask'), false, 'solo «ask» es Solo consulta')
assert.equal(chatModeValue(true), 'agent')
assert.equal(chatModeValue(false), 'ask')
assert.equal(isAgentMode(chatModeValue(false)), false, 'ida y vuelta')

// Las preferencias globales de antes (por tipo de proveedor) se descartan: ninguna deja una conversación en consulta.
assert.deepEqual([...LEGACY_MODE_KEYS].sort(), ['scadlab.chat.agent', 'scadlab.chat.agent.cli', 'scadlab.chat.mode.cli', 'scadlab.chat.mode.server'])
store.set('scadlab.chat.mode.cli', 'ask') // S12.c (T9)
store.set('scadlab.chat.mode.server', 'ask')
store.set('scadlab.chat.agent.cli', 'off') // S12.b
store.set('scadlab.chat.agent', 'off') // S12
store.set('scadlab.chat.provider', 'cli:claude') // otras preferencias del chat NO se tocan
store.set('scadlab.chat.model.cli:claude', 'opus')
store.set('scadlab.chat.maxSteps', '20')
assert.equal(clearLegacyChatMode(), 4, 'quita las cuatro claves viejas')
for (const k of LEGACY_MODE_KEYS) assert.equal(localStorage.getItem(k), null, `${k} sigue en localStorage`)
assert.equal(store.get('scadlab.chat.provider'), 'cli:claude', 'el proveedor recordado se conserva')
assert.equal(store.get('scadlab.chat.model.cli:claude'), 'opus', 'el modelo recordado se conserva')
assert.equal(store.get('scadlab.chat.maxSteps'), '20', 'el tope de pasos se conserva')
assert.equal(clearLegacyChatMode(), 0, 'es idempotente: la segunda vez no hay nada que quitar')
const closed = {
    getItem: () => {
        throw new Error('datos bloqueados')
    },
    removeItem: () => {
        throw new Error('datos bloqueados')
    },
}
assert.equal(clearLegacyChatMode(closed), 0, 'un almacenamiento bloqueado no rompe el arranque')
assert.equal(clearLegacyChatMode(null), 0, 'sin almacenamiento tampoco')

// ── ¿Pide crear o cambiar algo? (el aviso «Cambiar a Agente») ──
for (const yes of [
    'Una caja con tapa a presión para un Raspberry Pi 5',
    'Hazme un soporte para auriculares',
    'diseña una llave para el grifo',
    'Agrega agujeros M3 a este soporte',
    'Copia la pieza de esta foto',
    'A box with a snap-on lid',
    'Make a bracket for a 20 mm tube',
    'I need a lid for this box',
    'quiero una tapa más alta',
]) {
    assert.equal(asksToCreate(yes), true, yes)
}
for (const no of ['¿Cómo funciona el color() de OpenSCAD?', 'qué es un checkpoint', 'Explain what a minkowski does', 'gracias', '', '   ']) {
    assert.equal(asksToCreate(no), false, no)
}

// ── Qué checkpoint dejó un paso ──
const step = (tool, status, text, args = {}) => ({id: 's', index: 1, tool, args, status, text, images: [], durationMs: 0, textAt: 0})
assert.equal(createdCheckpoint(step('create_checkpoint', 'ok', '{"file":"002-caja.scad","activeParent":"002-caja.scad"}')), '002-caja.scad')
assert.equal(createdCheckpoint(step('autosave', 'ok', '{"file":"003-caja.scad","parent":"002-caja.scad"}')), '003-caja.scad', 'el guardado automático cuenta')
assert.equal(createdCheckpoint(step('import_mesh', 'ok', 'texto suelto de un CLI', {file: 'refs/x.scad'})), 'refs/x.scad', 'sin JSON, el argumento `file`')
assert.equal(createdCheckpoint(step('create_checkpoint', 'error', 'error: {"file":"x.scad"}')), '', 'un paso fallido no creó nada')
assert.equal(createdCheckpoint(step('create_checkpoint', 'running', '')), '', 'uno en curso, tampoco')
assert.equal(createdCheckpoint(step('render', 'ok', '{"file":"002-caja.scad"}', {file: '002-caja.scad'})), '', 'render no crea')

// Las herramientas que crean un checkpoint: set_parameters, create_from_template y edit_mesh también (su resultado trae `file`).
for (const tool of ['create_checkpoint', 'import_mesh', 'set_parameters', 'create_from_template', 'edit_mesh', 'autosave']) {
    assert.equal(CREATING_TOOLS.has(tool), true, `${tool} crea un checkpoint`)
    assert.equal(createdCheckpoint(step(tool, 'ok', '{"file":"004-nuevo.scad","parent":"003-base.scad"}', {file: '003-base.scad'})), '004-nuevo.scad', `${tool}: el checkpoint es el del resultado`)
}
for (const tool of ['set_parameters', 'edit_mesh']) {
    assert.equal(createdCheckpoint(step(tool, 'ok', 'texto suelto', {file: '003-base.scad'})), '', `${tool}: su argumento \`file\` es la base, no el nuevo`)
}
for (const tool of ['set_active_parent', 'rename_checkpoint', 'trash_checkpoint', 'restore_checkpoint', 'get_parameters', 'list_trash', 'export']) {
    assert.equal(CREATING_TOOLS.has(tool), false, `${tool} no crea un checkpoint`)
    assert.equal(createdCheckpoint(step(tool, 'ok', '{"file":"004-nuevo.scad"}', {file: '004-nuevo.scad'})), '', tool)
}
// El espejo en Go (backend/mcp/effects.go, EffectCheckpoint) dice lo mismo: una herramienta nueva que cree checkpoints
// se suma allá y aquí, y esta prueba avisa si quedaron distintas.
const effects = readFileSync(new URL('../../backend/mcp/effects.go', import.meta.url), 'utf8')
const goCreators = [...effects.matchAll(/"(\w+)":\s+EffectCheckpoint/g)].map((m) => m[1]).sort()
assert.ok(goCreators.length >= 5, 'no se pudo leer effects.go')
assert.deepEqual([...CREATING_TOOLS].filter((t) => t !== 'autosave').sort(), goCreators, 'CREATING_TOOLS = EffectCheckpoint de Go (+ autosave)')

// ── «Crear desde una imagen» (S15) ──
// El mensaje estructurado: el objeto, la referencia y la skill; sin datos, pide identificar el objeto y estimar la escala.
const full = fromImageMessage(es, {what: ' una escuadra en L ', reference: 'el ancho es 60 mm', count: 2})
assert.match(full, /las 2 fotos adjuntas/)
assert.match(full, /`openscad-from-image`/, 'nombra la skill')
assert.match(full, /- Objeto: una escuadra en L$/m, 'recorta los espacios')
assert.match(full, /- Medida de referencia: el ancho es 60 mm$/m)
const bare = fromImageMessage(es, {what: '  ', reference: '', count: 1})
assert.match(bare, /la foto adjunta/)
assert.match(bare, /identifícalo tú/, 'sin «qué es»: que lo identifique')
assert.match(bare, /estima la escala/, 'sin medida: que estime y lo diga')
assert.doesNotMatch(bare, /\{\w+\}/, 'ninguna variable sin reemplazar')
assert.match(fromImageMessage(en, {what: 'a bracket', reference: '', count: 3}), /the 3 attached photos[\s\S]*Object: a bracket[\s\S]*estimate the scale/)

console.log('chat-ux: OK')
