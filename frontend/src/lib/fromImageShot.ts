// Solo el banco de capturas (src/uishot.tsx): deja abierto el diálogo «Crear desde una imagen» con dos fotos
// inventadas, una medida y qué es, sin simular clics. En la app real nunca se escribe.
export const fromImageShot: {
    open?: boolean
    reference?: string
    what?: string
    photos?: {id: string; name: string; size: number; thumb: string}[]
} = {}
