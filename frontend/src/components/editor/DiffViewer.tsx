import {useEffect, useRef} from 'react'
import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import {monaco, monacoFontFamily, THEME} from './monaco'
import {LANGUAGE_ID} from './scadLanguage'
import {useT} from '../../i18n'

export type DiffViewerProps = {
    /** Código del padre (izquierda) y del checkpoint (derecha). */
    original: string
    modified: string
    /** Clave del par: al cambiar se vuelven a cargar los dos textos. */
    docKey: string
    sideBySide: boolean
    fontSize: number
    tabSize: number
}

// DiffViewer es el DiffEditor de Monaco con OpenSCAD, siempre en solo lectura:
// el diff no escribe nada: ni el checkpoint ni su padre (ni una versión del historial). Comparte tema, tipografía y
// textos con el editor de código. Entra con React.lazy, en el mismo chunk.
export default function DiffViewer(props: DiffViewerProps) {
    const t = useT()
    const host = useRef<HTMLDivElement>(null)
    const ed = useRef<Monaco.editor.IStandaloneDiffEditor | null>(null)
    const models = useRef<{original: Monaco.editor.ITextModel; modified: Monaco.editor.ITextModel} | null>(null)
    const cb = useRef(props)
    cb.current = props

    useEffect(() => {
        if (!host.current) return
        const p = cb.current
        const original = monaco.editor.createModel(p.original, LANGUAGE_ID)
        const modified = monaco.editor.createModel(p.modified, LANGUAGE_ID)
        original.updateOptions({tabSize: p.tabSize})
        modified.updateOptions({tabSize: p.tabSize})
        models.current = {original, modified}
        const e = monaco.editor.createDiffEditor(host.current, {
            theme: THEME,
            readOnly: true,
            originalEditable: false,
            automaticLayout: true,
            renderSideBySide: p.sideBySide,
            // La elección la hace el usuario con el botón del panel, no el ancho.
            useInlineViewWhenSpaceIsLimited: false,
            enableSplitViewResizing: true,
            ignoreTrimWhitespace: false,
            renderOverviewRuler: true,
            diffAlgorithm: 'advanced',
            hideUnchangedRegions: {enabled: true, contextLineCount: 3, minimumLineCount: 4, revealLineCount: 20},
            fontFamily: monacoFontFamily(),
            fontSize: p.fontSize,
            lineHeight: Math.round(p.fontSize * 1.55),
            fontLigatures: false,
            minimap: {enabled: false},
            scrollBeyondLastLine: false,
            padding: {top: 8, bottom: 8},
            glyphMargin: false,
            lineNumbersMinChars: 3,
            folding: false,
            scrollbar: {verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false},
            overviewRulerBorder: false,
            fixedOverflowWidgets: true,
            renderValidationDecorations: 'off',
        })
        e.setModel({original, modified})
        ed.current = e
        return () => {
            ed.current = null
            e.dispose()
            original.dispose()
            modified.dispose()
            models.current = null
        }
    }, [])

    // Otro par de checkpoints: se reemplaza el texto de los dos modelos.
    const loaded = useRef(props.docKey)
    useEffect(() => {
        const m = models.current
        if (!m || loaded.current === props.docKey) return
        loaded.current = props.docKey
        m.original.setValue(props.original)
        m.modified.setValue(props.modified)
    }, [props.docKey, props.original, props.modified])

    useEffect(() => {
        ed.current?.updateOptions({
            renderSideBySide: props.sideBySide,
            fontSize: props.fontSize,
            lineHeight: Math.round(props.fontSize * 1.55),
        })
        models.current?.original.updateOptions({tabSize: props.tabSize})
        models.current?.modified.updateOptions({tabSize: props.tabSize})
    }, [props.sideBySide, props.fontSize, props.tabSize, t])

    return <div ref={host} data-font-area="editor" className="selectable h-full w-full" />
}
