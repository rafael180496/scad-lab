// Puente entre la cabecera (botón PNG, ⌘E…) y el visor 3D. El visor entra con
// React.lazy y es quien tiene el canvas: al montarse registra acá su captura, y
// la cabecera la invoca sin conocerlo. null = no hay visor montado.
let capturer: (() => Promise<string>) | null = null

// setViewerCapture lo llama el visor. `fn` pide dónde guardar, escribe el PNG y
// devuelve la ruta ("" si el usuario cancela).
export function setViewerCapture(fn: (() => Promise<string>) | null) {
    capturer = fn
}

// captureViewer guarda la vista activa del visor; "" si cancela o no hay visor.
export async function captureViewer(): Promise<string> {
    return capturer ? capturer() : ''
}
