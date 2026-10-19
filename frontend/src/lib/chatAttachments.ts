// Adjuntos del chat: clasificación, topes y compatibilidad con el modelo.
//
// Espeja backend/hermes/attachments.go (qué es cada archivo y cuánto puede
// pesar). Se repite acá SOLO para decidir antes de subir: el backend vuelve a
// clasificar por el CONTENIDO y es quien manda. Un desacuerdo se resuelve a favor
// de Go (el chip toma el `kind` que devuelve).

import {locale} from '../i18n'

export type AttachmentKind = 'image' | 'model'

export const MAX_IMAGE_BYTES = 5 * 1024 * 1024
export const MAX_MODEL_BYTES = 64 * 1024 * 1024
export const MAX_ATTACHMENTS = 10

const IMAGE_MIMES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp'])
const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp'])
const MODEL_EXTS = new Set(['stl', '3mf', 'obj'])
const OTHER_IMAGE_EXTS = new Set(['bmp', 'ico', 'tif', 'tiff', 'heic', 'heif', 'avif', 'svg'])

function extOf(name: string): string {
    const i = name.lastIndexOf('.')
    return i >= 0 ? name.slice(i + 1).toLowerCase() : ''
}

export type GuessedKind = AttachmentKind | 'image-unsupported' | 'unsupported'

/** Qué es un archivo SIN leerlo. `image-unsupported` son las imágenes que Go rechaza (BMP, HEIC…). */
export function guessKind(file: Pick<File, 'name' | 'type'>): GuessedKind {
    const mime = (file.type || '').toLowerCase()
    const ext = extOf(file.name)
    if (IMAGE_MIMES.has(mime) || (!mime && IMAGE_EXTS.has(ext))) return 'image'
    if (mime.startsWith('image/') || OTHER_IMAGE_EXTS.has(ext)) return 'image-unsupported'
    if (MODEL_EXTS.has(ext)) return 'model'
    if (IMAGE_EXTS.has(ext)) return 'image'
    return 'unsupported'
}

export const limitFor = (kind: AttachmentKind): number => (kind === 'image' ? MAX_IMAGE_BYTES : MAX_MODEL_BYTES)

/** Lo que el servidor sabe del modelo elegido. `null` = todavía no se sabe nada. */
export type VisionInfo = {vision: boolean; visionKnown: boolean} | null

// Cómo se lleva un adjunto con el modelo elegido.
//   ok       lo lee
//   maybe    no se pudo confirmar (visionKnown=false): se manda igual y contesta el servidor
//   blocked  Go lo va a rechazar: se deshabilita Enviar
// Un modelo 3D siempre es `ok`: su informe viaja como texto; sin visión se omite
// solo la vista previa (el chip lo avisa en su título).
export type Compat = 'ok' | 'maybe' | 'blocked'

export function compatibility(kind: AttachmentKind, info: VisionInfo): Compat {
    if (kind === 'model' || !info) return 'ok'
    if (info.visionKnown) return info.vision ? 'ok' : 'blocked'
    return 'maybe'
}

/** Nombre para una captura pegada: el navegador le pone `image.png` a todas y dos chips iguales no se distinguen. */
export function pastedName(file: File, n: number): string {
    const generic = !file.name || /^image\.(png|jpe?g|gif|webp)$/i.test(file.name)
    if (!generic) return file.name
    const ext = (file.type.split('/')[1] || 'png').replace('jpeg', 'jpg')
    const d = new Date()
    const hhmmss = [d.getHours(), d.getMinutes(), d.getSeconds()].map((x) => String(x).padStart(2, '0')).join('')
    return `captura-${hhmmss}${n > 0 ? `-${n + 1}` : ''}.${ext}`
}

export function readAsDataURL(file: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result ?? ''))
        reader.onerror = () => reject(reader.error ?? new DOMException('', 'NotReadableError'))
        reader.readAsDataURL(file)
    })
}

/** Tamaño de un adjunto: «187 KB», «1,8 MB». (lib/bytes.formatBytes redondea a MB: una foto de 187 KB saldría «0 MB».) */
export function formatSize(n: number): string {
    if (n >= 1 << 20) return `${(n / (1 << 20)).toLocaleString(locale(), {maximumFractionDigits: 1})} MB`
    if (n >= 1 << 10) return `${Math.round(n / (1 << 10)).toLocaleString(locale())} KB`
    return `${n} B`
}
