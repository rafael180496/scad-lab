#!/usr/bin/env node
// Pruebas de lo puro del historial de un checkpoint (S16): cuándo se ofrece «Deshacer cambio de la IA», qué hace
// la interfaz ante `checkpoint:updated`, cómo se rotulan las versiones y que los textos de los dos idiomas
// llevan las mismas variables.
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {planUpdate, previousVersion, reasonKind, relativeTime, absoluteTime, undoVersion, versionDate, versionLabel} from '../src/lib/history.ts'
import {CREATING_TOOLS, createdCheckpoint} from '../src/lib/chatSteps.ts'
import es from '../src/i18n/es.ts'
import en from '../src/i18n/en.ts'

const ai = {id: '20261019-221500', savedAt: '2026-10-19T22:15:00-06:00', reason: 'ai', tool: 'update_checkpoint', note: 'ventilación', size: 10}
const restore = {id: '20261019-223000', savedAt: '2026-10-19T22:30:00-06:00', reason: 'restore', tool: '', note: '', size: 10}

// ── «Deshacer cambio de la IA» solo si la versión más nueva la guardó la IA ──
assert.equal(undoVersion([ai]), ai)
assert.equal(undoVersion([ai, restore]), ai, 'solo importa la primera (la más nueva)')
assert.equal(undoVersion([restore, ai]), null, 'después de restaurar, lo más nuevo es «restore»: no hay cambio de la IA que deshacer')
assert.equal(undoVersion([]), null)
assert.equal(undoVersion(null), null)
assert.equal(undoVersion(undefined), null)

// ── «Ver diferencias» compara con la más nueva guardada ──
assert.equal(previousVersion([restore, ai]), restore)
assert.equal(previousVersion([]), null)
assert.equal(previousVersion(null), null)

assert.equal(reasonKind('ai'), 'ai')
assert.equal(reasonKind('restore'), 'restore')
assert.equal(reasonKind('user'), 'other')

// ── checkpoint:updated ──
assert.equal(planUpdate({selected: '002-x.scad', file: '002-x.scad', dirty: false}), 'reload')
assert.equal(planUpdate({selected: '002-x.scad', file: '002-x.scad', dirty: true}), 'warn', 'con cambios sin guardar manda el borrador')
assert.equal(planUpdate({selected: '001-base.scad', file: '002-x.scad', dirty: false}), 'invalidate')
assert.equal(planUpdate({selected: '001-base.scad', file: '002-x.scad', dirty: true}), 'invalidate', 'otro archivo: el borrador no es de este')
assert.equal(planUpdate({selected: null, file: '002-x.scad', dirty: false}), 'invalidate')

// ── Fechas ──
assert.ok(versionDate(ai) instanceof Date)
assert.equal(versionDate({savedAt: 'no es una fecha'}), null)
assert.equal(versionDate({savedAt: undefined}), null)
const now = new Date('2026-10-19T22:20:00-06:00')
const at = (iso) => new Date(iso)
assert.equal(relativeTime(at('2026-10-19T22:19:50-06:00'), now, 'en-US'), 'now')
assert.equal(relativeTime(at('2026-10-19T22:15:00-06:00'), now, 'en-US'), '5 minutes ago')
assert.equal(relativeTime(at('2026-10-19T22:15:00-06:00'), now, 'es-ES'), 'hace 5 minutos')
assert.equal(relativeTime(at('2026-10-19T19:20:00-06:00'), now, 'en-US'), '3 hours ago')
assert.equal(relativeTime(at('2026-10-17T22:20:00-06:00'), now, 'en-US'), '2 days ago')
assert.equal(relativeTime(at('2026-08-01T10:00:00-06:00'), now, 'en-US'), absoluteTime(at('2026-08-01T10:00:00-06:00'), 'en-US'), 'pasada una semana, la fecha')

// ── Rótulos ──
assert.equal(versionLabel({when: 'Versión anterior · 19 oct', reason: 'IA', note: 'ventilación'}), 'Versión anterior · 19 oct · IA · ventilación')
assert.equal(versionLabel({when: 'a', reason: 'IA', note: ''}), 'a · IA', 'sin nota no queda un separador colgando')

// ── El paso update_checkpoint enlaza el archivo editado (T1 lo suma a CREATING_TOOLS) ──
assert.ok(CREATING_TOOLS.has('update_checkpoint'), 'update_checkpoint tiene que estar en CREATING_TOOLS (espejo de effects.go)')
assert.equal(createdCheckpoint({status: 'ok', tool: 'update_checkpoint', args: {file: '002-x.scad'}, text: '{"file":"002-x.scad","updated":true}'}), '002-x.scad')
assert.equal(createdCheckpoint({status: 'error', tool: 'update_checkpoint', args: {file: '002-x.scad'}, text: 'error: no existe'}), '')

// ── Textos: es y en llevan las mismas variables {x} ──
const vars = (s) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(',')
function walk(a, b, path) {
    for (const k of Object.keys(a)) {
        const p = `${path}.${k}`
        if (typeof a[k] === 'string') {
            assert.equal(typeof b[k], 'string', `${p} falta en en.ts`)
            assert.equal(vars(a[k]), vars(b[k]), `${p}: las variables de es y en no coinciden`)
        } else walk(a[k], b[k], p)
    }
}
walk(es.history, en.history, 'history')
assert.ok(es.settings.historyMax && en.settings.historyMax)

console.log('history.test.mjs: ok')
