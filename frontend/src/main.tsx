import {StrictMode} from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App from './App'
import {clearLegacyChatMode} from './lib/chatMode'
import {installScrollGuard} from './lib/scrollGuard'

installScrollGuard()
// El modo del chat ya es de cada conversación: se descartan las preferencias globales de antes (S12.c).
clearLegacyChatMode()

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <App />
    </StrictMode>,
)
