#!/usr/bin/env node
// Prueba del filtro de respuestas de consulta de la terminal (12.7): el
// `^[[?1;2c` que aparecía en la primera línea de un agente al reenganchar.
// Node ejecuta el .ts tal cual (la función no usa sintaxis de tipos).
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {stripQueryReplies} from '../src/components/terminal/queryReplies.ts'

const E = '\x1b'
const cases = [
    // [descripción, entrada, salida]
    ['DA1 de xterm', `${E}[?1;2c`, ''],
    ['DA1 sin parámetros de más', `${E}[?6c`, ''],
    ['DA2', `${E}[>0;276;0c`, ''],
    ['CPR', `${E}[24;80R`, ''],
    ['CPR de DEC', `${E}[?24;80R`, ''],
    ['DSR de estado', `${E}[0n`, ''],
    ['OSC 11 (color de fondo) cerrado con BEL', `${E}]11;rgb:1c1c/1c1c/1f1f\x07`, ''],
    ['OSC 10 (color de texto) cerrado con ST', `${E}]10;rgb:ffff/ffff/ffff${E}\\`, ''],
    ['OSC 12 (cursor)', `${E}]12;rgb:ffff/9999/0000\x07`, ''],
    ['DECRPM', `${E}[?2026;2$y`, ''],
    ['XTVERSION', `${E}P>|xterm.js(6.0.0)${E}\\`, ''],
    ['varias seguidas', `${E}[?1;2c${E}[>0;276;0c${E}[1;1R${E}]11;rgb:0/0/0\x07`, ''],
    ['entre texto tecleado', `ls${E}[?1;2c -la\r`, 'ls -la\r'],
    ['solo el texto', 'ls -la\r', 'ls -la\r'],
    ['vacío', '', ''],
    // Lo que SÍ es del teclado pasa intacto.
    ['flecha arriba', `${E}[A`, `${E}[A`],
    ['Ctrl+flecha derecha', `${E}[1;5C`, `${E}[1;5C`],
    ['Mayús+Tab', `${E}[Z`, `${E}[Z`],
    ['F5', `${E}[15~`, `${E}[15~`],
    ['inicio de pegado', `${E}[200~`, `${E}[200~`],
    ['fin de pegado', `${E}[201~`, `${E}[201~`],
    ['Alt+letra', `${E}c`, `${E}c`],
    ['Alt+[', `${E}[`, `${E}[`],
    ['Ctrl+C', '\x03', '\x03'],
    ['texto pegado con una c y una R', 'cat README.md; echo c R', 'cat README.md; echo c R'],
    ['acentos y emoji', 'diseña ñandú 😀', 'diseña ñandú 😀'],
]

let failed = 0
for (const [name, input, want] of cases) {
    const got = stripQueryReplies(input)
    try {
        assert.equal(got, want)
    } catch {
        failed++
        console.error(`FALLA ${name}: ${JSON.stringify(input)} → ${JSON.stringify(got)} (esperaba ${JSON.stringify(want)})`)
    }
}
// Idempotente: filtrar dos veces da lo mismo que una.
for (const [, input] of cases) assert.equal(stripQueryReplies(stripQueryReplies(input)), stripQueryReplies(input))

if (failed > 0) {
    console.error(`${failed} de ${cases.length} casos fallaron`)
    process.exit(1)
}
console.log(`terminal-replies: ${cases.length} casos bien`)
