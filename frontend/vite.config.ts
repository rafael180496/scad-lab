import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
    plugins: [react(), tailwindcss()],
    build: {
        // Wails sirve dist/ desde el binario: sin source maps en producción.
        sourcemap: false,
        chunkSizeWarningLimit: 2000,
    },
})
