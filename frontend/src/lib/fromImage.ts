import type {Dict} from '../i18n'

// «Crear desde una imagen» (S15): el mensaje estructurado que recibe el modelo. Va en el idioma de la interfaz
// (es lo que la persona ve en su conversación) y nombra la skill `openscad-from-image` para que el asistente la lea
// antes de empezar. Las rutas de las fotos no van aquí: las agrega el backend al final del mensaje (hermes.ImagePathsNote).

// Sin importar fmt de ../i18n: este archivo se prueba con Node (scripts/chat-ux.test.mjs), que no resuelve esa ruta.
const fill = (text: string, vars: Record<string, string | number>) => text.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m))

export type FromImageInput = {what: string; reference: string; count: number}

export function fromImageMessage(t: Dict, input: FromImageInput): string {
    const m = t.fromImage.message
    const what = input.what.trim()
    const reference = input.reference.trim()
    const photos = input.count === 1 ? m.photosOne : fill(m.photosMany, {n: input.count})
    return [
        fill(m.intro, {photos}),
        what ? fill(m.object, {what}) : m.objectNone,
        reference ? fill(m.reference, {reference}) : m.referenceNone,
    ].join('\n')
}
