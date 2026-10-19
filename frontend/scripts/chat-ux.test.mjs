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

console.log('chat-ux: OK')
