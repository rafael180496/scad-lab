import {realpathSync, rmSync} from 'node:fs'
import {resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {defineConfig, type Plugin} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// public/uishot/ son mallas de ejemplo SOLO para el banco de capturas
// (src/uishot.tsx): Vite copia public/ entero a dist/, y dist/ va embebido en
// el binario. Se borran del build.
function dropUishotAssets(): Plugin {
    let outDir = 'dist'
    return {
        name: 'scadlab-drop-uishot-assets',
        apply: 'build',
        configResolved(c) {
            outDir = resolve(c.root, c.build.outDir)
        },
        closeBundle() {
            rmSync(resolve(outDir, 'uishot'), {recursive: true, force: true})
        },
    }
}

export default defineConfig({
    plugins: [react(), tailwindcss(), dropUishotAssets()],
    resolve: {
        alias: [
            // El editor importa Monaco por archivo (núcleo + contribuciones
            // sueltas, ver src/components/editor/monaco.ts). El `exports` del
            // paquete mapea `./*` a `./esm/vs/*`, así que la ruta real
            // —la que entiende TypeScript— se resuelve a mano.
            // Con la ruta REAL (pnpm enlaza node_modules/monaco-editor a .pnpm/):
            // con la del enlace, los imports internos de Monaco resolverían a
            // otra URL y habría dos copias de sus registros (sin marcadores).
            {find: /^monaco-editor\/esm\/(.*)$/, replacement: realpathSync(fileURLToPath(new URL('./node_modules/monaco-editor/esm/', import.meta.url))) + '/$1'},
        ],
    },
    // En dev, Monaco no se pre-empaqueta: el optimizador de Vite no sabe
    // manejar el `?worker` del worker del editor y la página queda en blanco.
    optimizeDeps: {exclude: ['monaco-editor']},
    server: {
        // El banco de capturas importa el catálogo real del lenguaje
        // (assets/scad/catalog.json), fuera de frontend/. Solo en dev.
        fs: {allow: ['..']},
    },
    build: {
        // Wails sirve dist/ desde el binario: sin source maps en producción.
        sourcemap: false,
        chunkSizeWarningLimit: 2000,
    },
})
