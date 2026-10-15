#!/usr/bin/env node
// Chequeo de textos sin i18n en el frontend (ver claude/05-i18n.md).
// Busca texto literal entre etiquetas JSX y en atributos visibles
// (title, placeholder, aria-label, alt) que no pase por el diccionario.
// Uso: node frontend/scripts/i18n-check.mjs   (sale con 1 si encuentra algo)
import {readFileSync, readdirSync, statSync} from 'node:fs'
import {join, relative} from 'node:path'
import {fileURLToPath} from 'node:url'

const root = fileURLToPath(new URL('../src', import.meta.url))
// Nombres propios y símbolos que no se traducen.
const allow = new Set(['ScadLab', 'STL', '3MF', 'PNG', 'openscad', 'px', '/', '—', '·', '…'])

const files = []
const walk = (dir) => {
    for (const name of readdirSync(dir)) {
        const p = join(dir, name)
        if (statSync(p).isDirectory()) {
            if (name !== 'i18n') walk(p)
        } else if (p.endsWith('.tsx')) files.push(p)
    }
}
walk(root)

const textRe = />\s*([^<>{}\n]*[A-Za-zÁÉÍÓÚáéíóúñÑ]{2,}[^<>{}\n]*)\s*</g
const attrRe = /\b(title|placeholder|aria-label|alt)="([^"]*[A-Za-z]{2,}[^"]*)"/g
let problems = 0
for (const f of files) {
    const lines = readFileSync(f, 'utf8').split('\n')
    lines.forEach((line, i) => {
        if (line.trim().startsWith('//') || line.includes('i18n-ignore')) return
        for (const re of [textRe, attrRe]) {
            re.lastIndex = 0
            let m
            while ((m = re.exec(line))) {
                const text = (m[2] ?? m[1]).trim()
                if (!text || allow.has(text) || /^[=&|?:().,;\s\w]*=>/.test(text)) continue
                if (/[=;]|\)\s*$/.test(text)) continue // código, no texto
                console.log(`${relative(root, f)}:${i + 1}: texto sin i18n: ${JSON.stringify(text)}`)
                problems++
            }
        }
    })
}
if (problems) {
    console.error(`\n${problems} texto(s) sin pasar por el diccionario (src/i18n/es.ts y en.ts).`)
    process.exit(1)
}
console.log(`i18n OK — ${files.length} archivos revisados`)
