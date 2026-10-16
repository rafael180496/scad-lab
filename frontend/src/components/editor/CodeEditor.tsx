import {useEffect, useRef} from 'react'
import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import {monaco, monacoFontFamily, THEME} from './monaco'
import {LANGUAGE_ID} from './scadLanguage'
import {editorSession, registerEditor} from '../../lib/editorSession'
import type {Problem} from '../../lib/problems'
import {useT} from '../../i18n'

export type CodeEditorProps = {
    // docKey identifica el documento: al cambiar (otro checkpoint, o
    // «descartar cambios») se vuelve a cargar `value` y se pierde el deshacer.
    docKey: string
    value: string
    readOnly: boolean
    fontSize: number
    tabSize: number
    wordWrap: boolean
    minimap: boolean
    markers: Problem[]
    onChange: (text: string) => void
    onRequestEdit: () => void
    onFormat: () => void
    // Solo el banco de capturas (src/uishot.tsx): deja el editor mostrando el
    // autocompletado o buscar y reemplazar, sin simular teclas.
    shot?: 'edit' | 'search'
}

const SEVERITY: Record<Problem['severity'], Monaco.MarkerSeverity> = {
    error: monaco.MarkerSeverity.Error,
    warning: monaco.MarkerSeverity.Warning,
    info: monaco.MarkerSeverity.Info,
}

// CodeEditor es Monaco con OpenSCAD. Entra con React.lazy (chunk propio):
// Monaco pesa más que el resto de la interfaz junta.
export default function CodeEditor(props: CodeEditorProps) {
    const t = useT()
    const host = useRef<HTMLDivElement>(null)
    const ed = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null)
    const flash = useRef<Monaco.editor.IEditorDecorationsCollection | null>(null)
    // Los callbacks cambian en cada render del padre; el editor se crea una
    // sola vez y llama siempre a la versión vigente.
    const cb = useRef(props)
    cb.current = props
    const loadedKey = useRef('')

    useEffect(() => {
        if (!host.current) return
        const model = monaco.editor.createModel(cb.current.value, LANGUAGE_ID)
        loadedKey.current = cb.current.docKey
        const e = monaco.editor.create(host.current, {
            model,
            theme: THEME,
            readOnly: cb.current.readOnly,
            automaticLayout: true,
            fontFamily: monacoFontFamily(),
            fontSize: cb.current.fontSize,
            fontLigatures: false,
            lineHeight: Math.round(cb.current.fontSize * 1.55),
            tabSize: cb.current.tabSize,
            insertSpaces: true,
            detectIndentation: false,
            wordWrap: cb.current.wordWrap ? 'on' : 'off',
            minimap: {enabled: cb.current.minimap},
            scrollBeyondLastLine: false,
            renderLineHighlight: 'all',
            smoothScrolling: true,
            padding: {top: 8, bottom: 8},
            glyphMargin: false,
            // Por defecto Monaco no pinta los marcadores en solo lectura, que
            // es justo como se abre un checkpoint.
            renderValidationDecorations: 'on',
            folding: true,
            lineNumbersMinChars: 3,
            fixedOverflowWidgets: true,
            bracketPairColorization: {enabled: false},
            guides: {indentation: true, bracketPairs: false},
            suggest: {showWords: true, preview: true},
            parameterHints: {enabled: true},
            quickSuggestions: {other: true, comments: false, strings: false},
            find: {addExtraSpaceOnTop: false, seedSearchStringFromSelection: 'selection'},
            scrollbar: {verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false},
            overviewRulerBorder: false,
            hideCursorInOverviewRuler: true,
        })
        // Colores de llaves propios del tema, no la paleta de VS Code.
        model.updateOptions({bracketColorizationOptions: {enabled: false, independentColorPoolPerBracketType: false}})
        ed.current = e
        flash.current = e.createDecorationsCollection()

        const subs = [
            e.onDidChangeModelContent(() => cb.current.onChange(e.getValue())),
            e.onDidChangeCursorPosition((ev) => editorSession.set({line: ev.position.lineNumber, col: ev.position.column})),
            // Doble clic en solo lectura = pasar a edición.
            e.onMouseDown((ev) => {
                if (ev.event.detail === 2 && e.getOption(monaco.editor.EditorOption.readOnly)) cb.current.onRequestEdit()
            }),
        ]
        // Formatear funciona también en solo lectura: pasa a edición y formatea.
        e.addCommand(monaco.KeyMod.Shift | monaco.KeyMod.Alt | monaco.KeyCode.KeyF, () => cb.current.onFormat())
        // Reemplazar con Cmd/Ctrl+H (en macOS Monaco usa ⌥⌘F; los dos sirven).
        e.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyH, () => {
            void e.getAction('editor.action.startFindReplaceAction')?.run()
        })

        registerEditor({
            revealLine(line, col) {
                const m = e.getModel()
                if (!m) return
                const ln = Math.min(Math.max(1, line), m.getLineCount())
                const c = col && col > 0 ? Math.min(col, m.getLineMaxColumn(ln)) : m.getLineFirstNonWhitespaceColumn(ln) || 1
                e.setPosition({lineNumber: ln, column: c})
                e.revealLineInCenter(ln, monaco.editor.ScrollType.Smooth)
                flash.current?.set([{range: new monaco.Range(ln, 1, ln, 1), options: {isWholeLine: true, className: 'scad-flash-line'}}])
                window.setTimeout(() => flash.current?.clear(), 1600)
                e.focus()
            },
            async format() {
                e.updateOptions({readOnly: false})
                await e.getAction('editor.action.formatDocument')?.run()
            },
            find(replace) {
                const id = replace && !e.getOption(monaco.editor.EditorOption.readOnly) ? 'editor.action.startFindReplaceAction' : 'actions.find'
                void e.getAction(id)?.run()
            },
            focus: () => e.focus(),
            getValue: () => e.getValue(),
        })
        editorSession.set({line: 1, col: 1})

        return () => {
            registerEditor(null)
            subs.forEach((s) => s.dispose())
            e.dispose()
            model.dispose()
            ed.current = null
        }
    }, [])

    useEffect(() => {
        const e = ed.current
        const shot = props.shot
        if (!e || !shot) return
        const timer = window.setTimeout(() => {
            const m = e.getModel()
            if (!m) return
            e.updateOptions({readOnly: false})
            e.focus()
            if (shot === 'search') {
                const hit = m.findMatches('wall', false, false, true, null, false)[0]
                if (hit) e.setSelection(hit.range)
                void e.getAction('editor.action.startFindReplaceAction')?.run()
                return
            }
            // Una pieza nueva a medio escribir, antes del primer `module`.
            const at = m.findMatches('module ', false, false, true, null, false)[0]?.range.startLineNumber ?? m.getLineCount()
            const text = 'translate([width / 2, depth / 2, 0])\n    cyl\n\n'
            e.executeEdits('uishot', [{range: new monaco.Range(at, 1, at, 1), text}])
            e.setPosition({lineNumber: at + 1, column: 8})
            e.revealLineInCenter(at)
            void e.getAction('editor.action.triggerSuggest')?.run()
            window.setTimeout(() => e.trigger('uishot', 'toggleSuggestionDetails', null), 800)
        }, 200)
        return () => window.clearTimeout(timer)
    }, [props.shot])

    // Documento nuevo: se reemplaza el contenido y se vuelve arriba.
    useEffect(() => {
        const e = ed.current
        if (!e || loadedKey.current === props.docKey) return
        loadedKey.current = props.docKey
        e.getModel()?.setValue(props.value)
        e.setScrollPosition({scrollTop: 0, scrollLeft: 0})
        e.setPosition({lineNumber: 1, column: 1})
    }, [props.docKey, props.value])

    useEffect(() => {
        ed.current?.updateOptions({
            readOnly: props.readOnly,
            readOnlyMessage: {value: t.editor.readOnlyMessage},
            fontSize: props.fontSize,
            lineHeight: Math.round(props.fontSize * 1.55),
            tabSize: props.tabSize,
            wordWrap: props.wordWrap ? 'on' : 'off',
            minimap: {enabled: props.minimap},
        })
    }, [props.readOnly, props.fontSize, props.tabSize, props.wordWrap, props.minimap, t])

    useEffect(() => {
        const model = ed.current?.getModel()
        if (!model) return
        const lines = model.getLineCount()
        const markers: Monaco.editor.IMarkerData[] = props.markers
            .filter((p) => p.line > 0 && !p.file)
            .map((p) => {
                const line = Math.min(p.line, lines)
                const whole = p.endLine <= 0
                const startColumn = whole ? model.getLineFirstNonWhitespaceColumn(line) || 1 : Math.max(1, p.col)
                const endLine = whole ? line : Math.min(p.endLine, lines)
                const endColumn = whole ? model.getLineMaxColumn(line) : Math.max(p.endCol, startColumn + 1)
                return {
                    severity: SEVERITY[p.severity],
                    message: p.message,
                    code: p.code || undefined,
                    source: p.origin === 'render' ? 'OpenSCAD render' : p.code === 'syntax' ? 'OpenSCAD' : 'ScadLab',
                    startLineNumber: line,
                    startColumn,
                    endLineNumber: endLine,
                    endColumn,
                }
            })
        monaco.editor.setModelMarkers(model, 'scadlab', markers)
    }, [props.markers, props.docKey])

    return <div ref={host} data-font-area="editor" className="selectable h-full w-full" />
}
