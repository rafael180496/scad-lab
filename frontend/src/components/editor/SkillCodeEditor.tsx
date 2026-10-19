import {useEffect, useRef} from 'react'
import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import {monaco, monacoFontFamily, THEME} from './monaco'
import {LANGUAGE_ID} from './scadLanguage'

export type SkillLanguage = 'markdown' | 'openscad' | 'plaintext'

export type SkillMarker = {line: number; severity: 'error' | 'warning'; message: string; code?: string}

export type SkillCodeEditorProps = {
    /** Un documento por archivo de la skill: cada uno conserva su propio historial de deshacer. */
    docKey: string
    /** Texto con el que se crea el documento la primera vez que se abre `docKey`. */
    value: string
    language: SkillLanguage
    readOnly: boolean
    fontSize: number
    tabSize: number
    wordWrap: boolean
    markers: SkillMarker[]
    onChange: (text: string) => void
    /** Lleva el cursor a esta línea; `nonce` distingue dos pedidos a la misma línea. */
    reveal?: {line: number; nonce: number} | null
}

const SEVERITY: Record<SkillMarker['severity'], Monaco.MarkerSeverity> = {
    error: monaco.MarkerSeverity.Error,
    warning: monaco.MarkerSeverity.Warning,
}

// i18n-ignore: ids de lenguaje de Monaco
const LANGUAGE_IDS: Record<SkillLanguage, string> = {markdown: 'markdown', openscad: LANGUAGE_ID, plaintext: 'plaintext'}

// SkillCodeEditor es el Monaco de Configuración → Skills: el mismo Monaco local
// del editor de código (nada de CDN), pero para archivos sueltos de una skill —
// Markdown u OpenSCAD según la extensión— y sin lo propio de un checkpoint
// (linter de OpenSCAD, formateador, sesión compartida). Entra con React.lazy,
// en el chunk de Monaco.
export default function SkillCodeEditor(props: SkillCodeEditorProps) {
    const host = useRef<HTMLDivElement>(null)
    const ed = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null)
    const models = useRef(new Map<string, Monaco.editor.ITextModel>())
    // El editor se crea una sola vez y llama siempre a la versión vigente.
    const cb = useRef(props)
    cb.current = props

    const modelFor = (key: string, value: string, language: SkillLanguage) => {
        let m = models.current.get(key)
        if (!m) {
            m = monaco.editor.createModel(value, LANGUAGE_IDS[language])
            models.current.set(key, m)
        }
        return m
    }

    useEffect(() => {
        if (!host.current) return
        const p = cb.current
        const e = monaco.editor.create(host.current, {
            model: modelFor(p.docKey, p.value, p.language),
            theme: THEME,
            readOnly: p.readOnly,
            automaticLayout: true,
            fontFamily: monacoFontFamily(),
            fontSize: p.fontSize,
            fontLigatures: false,
            lineHeight: Math.round(p.fontSize * 1.55),
            tabSize: p.tabSize,
            insertSpaces: true,
            detectIndentation: false,
            wordWrap: p.wordWrap ? 'on' : 'off',
            minimap: {enabled: false},
            scrollBeyondLastLine: false,
            renderLineHighlight: 'all',
            smoothScrolling: true,
            padding: {top: 8, bottom: 8},
            glyphMargin: false,
            renderValidationDecorations: 'on',
            folding: true,
            lineNumbersMinChars: 3,
            // Dentro de un <dialog> (capa superior) los widgets que Monaco saca al
            // <body> quedarían detrás del diálogo: se dejan dentro del editor.
            fixedOverflowWidgets: false,
            bracketPairColorization: {enabled: false},
            guides: {indentation: true, bracketPairs: false},
            quickSuggestions: false,
            suggestOnTriggerCharacters: false,
            find: {addExtraSpaceOnTop: false, seedSearchStringFromSelection: 'selection'},
            scrollbar: {verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false},
            overviewRulerBorder: false,
            hideCursorInOverviewRuler: true,
        })
        ed.current = e
        const sub = e.onDidChangeModelContent(() => cb.current.onChange(e.getValue()))
        const store = models.current
        return () => {
            sub.dispose()
            e.dispose()
            store.forEach((m) => m.dispose())
            store.clear()
            ed.current = null
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    // Otro archivo: se cambia de documento (el anterior conserva su deshacer).
    useEffect(() => {
        const e = ed.current
        if (!e) return
        const m = modelFor(props.docKey, props.value, props.language)
        if (e.getModel() !== m) {
            e.setModel(m)
            e.setScrollPosition({scrollTop: 0, scrollLeft: 0})
        }
        // `value` solo cuenta al crear el documento.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [props.docKey, props.language])

    useEffect(() => {
        ed.current?.updateOptions({
            readOnly: props.readOnly,
            fontSize: props.fontSize,
            lineHeight: Math.round(props.fontSize * 1.55),
            tabSize: props.tabSize,
            wordWrap: props.wordWrap ? 'on' : 'off',
        })
    }, [props.readOnly, props.fontSize, props.tabSize, props.wordWrap])

    useEffect(() => {
        const model = ed.current?.getModel()
        if (!model) return
        const lines = model.getLineCount()
        const data: Monaco.editor.IMarkerData[] = props.markers.map((mk) => {
            const line = Math.min(Math.max(1, mk.line), lines)
            return {
                severity: SEVERITY[mk.severity],
                message: mk.message,
                code: mk.code,
                source: 'ScadLab',
                startLineNumber: line,
                startColumn: model.getLineFirstNonWhitespaceColumn(line) || 1,
                endLineNumber: line,
                endColumn: model.getLineMaxColumn(line),
            }
        })
        monaco.editor.setModelMarkers(model, 'scadlab-skill', data)
    }, [props.markers, props.docKey])

    useEffect(() => {
        const e = ed.current
        const r = props.reveal
        const m = e?.getModel()
        if (!e || !r || !m) return
        const ln = Math.min(Math.max(1, r.line), m.getLineCount())
        e.setPosition({lineNumber: ln, column: m.getLineFirstNonWhitespaceColumn(ln) || 1})
        e.revealLineInCenter(ln, monaco.editor.ScrollType.Smooth)
        e.focus()
    }, [props.reveal])

    return <div ref={host} data-font-area="editor" className="selectable h-full w-full" />
}
