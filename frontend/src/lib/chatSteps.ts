import type {ChatStep} from './chatTypes'

// Qué checkpoint dejó un paso del chat. Lo usan la tarjeta del paso (el enlace) y
// la selección automática del visor y de Código (S12.c): venga el checkpoint de una
// herramienta del modelo, de un CLI o de la red de seguridad («Guardado
// automáticamente»), el resultado lo dice igual en su campo `file`.

/**
 * Pasos que CREAN un checkpoint: las herramientas del servidor que dejan un `.scad` nuevo (su resultado
 * trae `file`) y el guardado automático de ScadLab. Espejo de `Effect = EffectCheckpoint` en
 * backend/mcp/effects.go: una herramienta nueva que cree checkpoints se suma allá y aquí.
 * `edit_checkpoint`, `extract_module` y `update_checkpoint` reescriben en el lugar (S16, S17): su
 * resultado trae `file` y la tarjeta enlaza el archivo editado.
 */
export const CREATING_TOOLS: ReadonlySet<string> = new Set([
    'create_checkpoint',
    'update_checkpoint',
    'edit_checkpoint',
    'extract_module',
    'import_mesh',
    'set_parameters',
    'create_from_template',
    'edit_mesh',
    'autosave',
])

/** Las que reciben un checkpoint de entrada en `file` (su base): ese argumento NO es el checkpoint que crean. */
const FILE_ARG_IS_INPUT: ReadonlySet<string> = new Set(['set_parameters', 'edit_mesh'])

// Un `lib/x.scad` (S17, edit_checkpoint sobre una librería) no es un nodo del árbol: no se enlaza ni se selecciona.
const SCAD = /^(?!lib[/\\]).*\.scad$/i

/** El resultado de una herramienta como objeto JSON, o null si no lo es. */
export function stepResult(text: string): Record<string, unknown> | null {
    const t = text.trim().replace(/^error:\s*/, '')
    if (!t.startsWith('{')) return null
    try {
        const v = JSON.parse(t) as unknown
        return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
    } catch {
        return null
    }
}

/**
 * El checkpoint que un paso TERMINADO BIEN acaba de crear, o '' si no creó ninguno
 * (otra herramienta, un paso en curso o fallido). Mira el resultado y, si el CLI no
 * lo devolvió como JSON, el argumento `file`.
 */
export function createdCheckpoint(step: ChatStep): string {
    if (step.status !== 'ok' || !CREATING_TOOLS.has(step.tool)) return ''
    const r = stepResult(step.text)
    for (const f of [r?.file, FILE_ARG_IS_INPUT.has(step.tool) ? undefined : step.args.file]) {
        if (typeof f === 'string' && SCAD.test(f.trim())) return f.trim()
    }
    return ''
}
