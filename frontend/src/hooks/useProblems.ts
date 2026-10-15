import {useMemo} from 'react'
import type {render} from '../../wailsjs/go/models'
import {editorSession} from '../lib/editorSession'
import {mergeProblems, type Problem} from '../lib/problems'

// useProblems: linter (en vivo, desde el editor) + último render del
// checkpoint abierto. Lo usan el panel Problemas y la barra de estado, que se
// suscriben solos: el resto de la ventana no se redibuja con cada pasada del
// linter.
export function useProblems(renderDiags: render.Diagnostic[] | undefined): Problem[] {
    const lint = editorSession.use((s) => s.lint)
    return useMemo(() => mergeProblems(lint, renderDiags), [lint, renderDiags])
}
