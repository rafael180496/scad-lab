#!/usr/bin/env node
// Prueba del indicador de fuerza de la contraseña maestra (12.10). Es una guía,
// pero tiene que ser coherente: nada por debajo del largo mínimo pasa de «muy
// débil», las contraseñas de manual tampoco, y una larga y variada llega a «fuerte».
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {MIN_PASSWORD_LEN, passwordStrength} from '../src/lib/passwordStrength.ts'

assert.equal(MIN_PASSWORD_LEN, 8, 'el piso del frontend es el del backend (vaultgate.MinPasswordLen)')

const cases = [
    // [descripción, entrada, nivel esperado]
    ['vacía', '', 0],
    ['una letra', 'a', 1],
    ['bajo el mínimo aunque variada', 'Ab1!xy', 1],
    ['siete caracteres', 'Ab1!xyz', 1],
    ['de manual', 'password1234', 1],
    ['de manual en español', 'Contraseña2026', 1],
    ['solo dos caracteres repetidos', 'abababababab', 1],
    ['secuencia', '12345678901', 1],
    ['ocho letras minúsculas', 'tornillo', 2],
    ['diez letras minúsculas', 'tornillosx', 2],
    ['doce letras minúsculas', 'tornillosxyz', 3],
    ['corta pero con tres clases', 'Tornillo-4', 3],
    ['larga con tres clases', 'Bosque-azul-42', 4],
    ['frase larga', 'ruta del bosque azul de noviembre', 4],
]
for (const [name, input, want] of cases) {
    assert.equal(passwordStrength(input), want, `${name}: «${input}»`)
}
console.log(`password-strength: ${cases.length} casos bien`)
