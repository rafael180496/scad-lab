import {lazy} from 'react'
import {getLanguage} from '../../i18n'

// El editor entra con React.lazy: Monaco va en su propio chunk y la app abre
// sin cargarlo.
//
// Los textos de los widgets de Monaco (buscar y reemplazar, menú contextual,
// avisos) vienen de su propio catálogo de mensajes, empaquetado local. Monaco
// los lee al evaluar sus módulos, así que el de español tiene que cargarse
// ANTES que Monaco: cambiar de idioma con el editor ya cargado los deja en el
// idioma anterior hasta reiniciar la app (el resto de la interfaz sí cambia).
function loadNls() {
    return getLanguage() === 'es' ? import('monaco-editor/esm/vs/nls/lang/es.js') : Promise.resolve()
}

export function loadCodeEditor() {
    return loadNls().then(() => import('./CodeEditor'))
}

export const CodeEditor = lazy(loadCodeEditor)

// El visor de diferencias (comparar un checkpoint con su padre) comparte el
// chunk de Monaco con el editor: no suma otro.
export function loadDiffViewer() {
    return loadNls().then(() => import('./DiffViewer'))
}

export const DiffViewer = lazy(loadDiffViewer)
