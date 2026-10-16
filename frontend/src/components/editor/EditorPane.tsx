import {Suspense, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {createPortal} from 'react-dom'
import {useNarrow} from '../../hooks/useNarrow'
import {ArrowLeft, Columns2, Eye, Loader2, Lock, PencilLine, Rows2, Save, Search, Undo2, WandSparkles} from 'lucide-react'
import type {main, render, store} from '../../../wailsjs/go/models'
import {LintCheckpointSource, SaveAsCheckpoint} from '../../../wailsjs/go/main/App'
import {PanelToolbar, Placeholder} from '../layout/Pane'
import {Button, IconButton} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {CodeEditor, DiffViewer} from './lazy'
import {SaveCheckpointDialog} from './SaveCheckpointDialog'
import {EditorBoundary} from './EditorBoundary'
import {editorCommands, editorSession} from '../../lib/editorSession'
import {mergeProblems} from '../../lib/problems'
import {errorText} from '../../lib/errors'
import {isMod, withKeys} from '../../lib/platform'
import {fmt, useT, type Dict} from '../../i18n'
import {cn} from '../../lib/cn'

// EditorShot: estado inicial para el banco de capturas (src/uishot.tsx).
export type EditorShot = {mode: 'edit' | 'search' | 'save'; label?: string; description?: string}

// EditorSnap: lo que el editor tenía cuando el proyecto pasó a segundo plano (S8).
// Cada proyecto abierto conserva su código sin guardar, si estaba editando y la
// comparación con el padre; al volver a su pestaña EditorPane lo aplica una vez.
export type EditorSnap = {
    file: string
    /** Código que había en el editor (null = no se pudo leer: vale el guardado). */
    text: string | null
    editing: boolean
    dirty: boolean
    diff: main.CheckpointPair | null
}

// Espera desde la última tecla hasta revisar el código con el linter.
const LINT_DEBOUNCE_MS = 400

// confirmLeave pregunta antes de perder cambios sin guardar (cambiar de
// checkpoint o de workspace). true = se puede seguir.
export async function confirmLeave(t: Dict, file: string | null) {
    if (!editorSession.get().dirty) return true
    const ok = await confirmDialog(fmt(t.editor.discardConfirm, {file: file ?? ''}), t.editor.discard, t.editor.keepEditing)
    if (ok) editorSession.set({dirty: false, editing: false})
    return ok
}

// EditorPane es el panel de código: cabecera con el modo y las acciones, y
// Monaco debajo (que entra con lazy). Los checkpoints son inmutables: el
// código se abre en solo lectura y «Guardar» crea un checkpoint hijo.
// Ancho del panel de código (a interfaz 100 %) por debajo del cual la barra pasa a solo ícono.
const EDITOR_COMPACT_W = 400
// Ancho del panel (a interfaz 100 %) desde el que la comparación con el padre se
// abre lado a lado; por debajo, en línea. El botón del panel lo cambia a mano.
const DIFF_SIDE_BY_SIDE_W = 640

export function EditorPane({
    workspaceId,
    file,
    source,
    renderDiags,
    settings,
    onSaved,
    restore,
    onRestored,
    shot,
}: {
    workspaceId: number | null
    file: string | null
    source: string
    renderDiags: render.Diagnostic[] | undefined
    settings: store.Settings
    onSaved: (fileName: string) => void
    /** Estado que dejó este proyecto al irse a segundo plano (se aplica al volver y se consume). */
    restore?: EditorSnap | null
    onRestored?: () => void
    shot?: EditorShot
}) {
    const t = useT()
    // Panel angosto (o interfaz agrandada): los botones pierden el rótulo y
    // quedan como íconos con su `title`.
    const [compact, paneRef] = useNarrow(EDITOR_COMPACT_W)
    const [narrowDiff, diffRef] = useNarrow(DIFF_SIDE_BY_SIDE_W)
    const measure = useCallback(
        (el: HTMLElement | null) => {
            paneRef(el)
            diffRef(el)
        },
        [paneRef, diffRef],
    )
    const editing = editorSession.use((s) => s.editing)
    const dirty = editorSession.use((s) => s.dirty)
    const lint = editorSession.use((s) => s.lint)
    const diff = editorSession.use((s) => s.diff)
    // true = lado a lado; null = lo que decida el ancho (angosto: en línea)
    const [diffSide, setDiffSide] = useState<boolean | null>(null)
    const [reset, setReset] = useState(0)
    const [saveOpen, setSaveOpen] = useState(false)
    const [saving, setSaving] = useState(false)
    const sourceRef = useRef(source)
    sourceRef.current = source
    const lintSeq = useRef(0)
    const lintTimer = useRef<number | undefined>(undefined)

    const docKey = `${workspaceId ?? ''}:${file ?? ''}:${reset}`

    // El código a cargar al abrir el documento: el borrador que dejó el proyecto,
    // si lo hay, y si no lo guardado. Se fija por docKey (y no se lee de `restore`
    // en cada render) porque `restore` se consume apenas se aplica.
    const restoreRef = useRef(restore)
    restoreRef.current = restore
    const onRestoredRef = useRef(onRestored)
    onRestoredRef.current = onRestored
    const draft = useRef<{key: string; text: string} | null>(null)
    if (restore && restore.file === file && restore.text != null && draft.current?.key !== docKey) {
        draft.current = {key: docKey, text: restore.text}
    }
    const value = draft.current?.key === docKey ? draft.current.text : source

    // La comparación es de un checkpoint: al elegir otro (o ninguno) se cierra.
    const showDiff = diff != null && diff.file === file
    useEffect(() => {
        if (diff && diff.file !== file) editorSession.set({diff: null})
    }, [diff, file])

    const runLint = useCallback(
        (text: string) => {
            if (workspaceId == null || !file) return
            const seq = ++lintSeq.current
            editorSession.set({linting: true})
            LintCheckpointSource(workspaceId, file, text)
                .then((diags) => {
                    if (seq === lintSeq.current) editorSession.set({lint: diags ?? [], linting: false})
                })
                .catch(() => {
                    // Un linter caído no debe tapar el editor: se queda sin marcadores.
                    if (seq === lintSeq.current) editorSession.set({lint: [], linting: false})
                })
        },
        [workspaceId, file],
    )

    // Documento nuevo (otro checkpoint, o descartar): solo lectura, sin
    // cambios, y una pasada del linter sobre lo guardado. El código llega en
    // el mismo render que el nombre (App lee el archivo antes de elegirlo).
    const lastKey = useRef('')
    useEffect(() => {
        if (lastKey.current !== docKey) {
            lastKey.current = docKey
            window.clearTimeout(lintTimer.current)
            const r = restoreRef.current
            if (r && r.file === file) {
                // Vuelve a su pestaña un proyecto con borrador, edición o comparación abiertos.
                editorSession.set({editing: r.editing, dirty: r.dirty, lint: [], diff: r.diff})
                onRestoredRef.current?.()
                if (r.editing && file) runLint(r.text ?? source)
            } else {
                editorSession.set({editing: false, dirty: false, lint: []})
            }
        }
        if (!file) {
            lintSeq.current++
            return
        }
        if (!editorSession.get().editing) runLint(source)
    }, [docKey, source, file, runLint])
    // Un `restore` que llega sin que cambie el documento (se fue y volvió al proyecto antes
    // de que el editor viera el otro) no tiene nada que aplicar: el editor ya está así.
    useEffect(() => {
        if (!restore || lastKey.current !== docKey) return
        if (restore.diff) editorSession.set({diff: restore.diff})
        onRestoredRef.current?.()
    }, [restore, docKey])
    useEffect(() => () => window.clearTimeout(lintTimer.current), [])

    const onChange = useCallback(
        (text: string) => {
            const d = text !== sourceRef.current
            editorSession.set({dirty: d})
            if (!editorSession.get().editing) return
            window.clearTimeout(lintTimer.current)
            lintTimer.current = window.setTimeout(() => runLint(text), LINT_DEBOUNCE_MS)
        },
        [runLint],
    )

    // Banco de capturas: entra directo en el estado a fotografiar.
    useEffect(() => {
        if (!shot || !file) return
        editorSession.set({editing: true})
        if (shot.mode === 'save') {
            editorSession.set({dirty: true})
            setSaveOpen(true)
        }
    }, [shot, file])

    const startEditing = useCallback(() => {
        if (file) editorSession.set({editing: true})
    }, [file])

    const format = useCallback(async () => {
        if (!file) return
        startEditing()
        await editorCommands()?.format()
    }, [file, startEditing])

    const discard = async () => {
        if (dirty) {
            const ok = await confirmDialog(fmt(t.editor.discardConfirm, {file: file ?? ''}), t.editor.discard, t.editor.keepEditing)
            if (!ok) return
        }
        setReset((n) => n + 1)
    }

    const openSave = useCallback(() => {
        if (!editorSession.get().editing) return
        if (!editorSession.get().dirty) {
            toast.ok(t.editor.noChanges)
            return
        }
        setSaveOpen(true)
    }, [t])

    // Cmd/Ctrl+S guarda desde cualquier lado de la ventana, no solo con el
    // foco en el editor (en captura: antes de que Monaco lo vea).
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (isMod(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 's') {
                e.preventDefault()
                e.stopPropagation()
                openSave()
            }
        }
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [openSave])

    const save = async (label: string, description: string) => {
        const cmd = editorCommands()
        if (workspaceId == null || !file || !cmd) return
        setSaving(true)
        try {
            const name = await SaveAsCheckpoint(workspaceId, file, label, description, cmd.getValue())
            setSaveOpen(false)
            editorSession.set({dirty: false, editing: false})
            toast.ok(fmt(t.editor.saved, {file: name}))
            onSaved(name)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setSaving(false)
        }
    }

    // En el editor, los diagnósticos del render solo mientras el código es el
    // que se renderizó: con cambios, sus líneas ya no coinciden.
    const markers = useMemo(() => mergeProblems(lint, dirty ? undefined : renderDiags), [lint, dirty, renderDiags])

    const small = 'h-6 px-2 text-ui-11-5'
    const sideBySide = diffSide ?? !narrowDiff
    const diffActions = diff ? (
        <>
            <IconButton
                title={sideBySide ? t.checkpoints.diff.inlineHint : t.checkpoints.diff.sideBySideHint}
                className="size-6"
                onClick={() => setDiffSide(!sideBySide)}
            >
                {sideBySide ? <Rows2 className="icon-ui-14" /> : <Columns2 className="icon-ui-14" />}
            </IconButton>
            <Button className={small} title={fmt(t.checkpoints.diff.backHint, {file: diff.file})} onClick={() => editorSession.set({diff: null})}>
                <ArrowLeft className="icon-ui-12" />
                {compact ? null : t.checkpoints.diff.back}
            </Button>
        </>
    ) : null
    const actions = file ? (
        <>
            <span
                className={cn(
                    'mr-1 flex shrink-0 items-center gap-1 rounded px-1.5 py-px text-ui-11',
                    editing ? 'bg-accent-soft text-accent' : 'text-faint',
                )}
                title={editing ? t.editor.editingHint : t.editor.readOnlyHint}
            >
                {editing ? <PencilLine className="icon-ui-12" /> : <Lock className="icon-ui-12" />}
                {compact ? null : editing ? t.editor.editing : t.editor.readOnly}
                {dirty ? <span className="size-1.5 rounded-full bg-accent" aria-label={t.editor.unsaved} /> : null}
            </span>
            <IconButton title={withKeys(t.editor.findHint, 'mod+f')} className="size-6" onClick={() => editorCommands()?.find(false)}>
                <Search className="icon-ui-14" />
            </IconButton>
            <IconButton title={withKeys(t.editor.formatHint, 'shift+alt+f')} className="size-6" onClick={() => void format()}>
                <WandSparkles className="icon-ui-14" />
            </IconButton>
            {!editing ? (
                <Button className={small} title={t.editor.editHint} onClick={startEditing}>
                    <PencilLine className="icon-ui-12" />
                    {compact ? null : t.editor.edit}
                </Button>
            ) : dirty ? (
                <>
                    <IconButton title={t.editor.discardHint} className="size-6" onClick={() => void discard()}>
                        <Undo2 className="icon-ui-14" />
                    </IconButton>
                    <Button variant="primary" className={small} title={withKeys(fmt(t.editor.saveHint, {file}), 'mod+s')} onClick={openSave}>
                        <Save className="icon-ui-12" />
                        {t.editor.save}
                    </Button>
                </>
            ) : (
                <Button className={small} title={t.editor.viewHint} onClick={() => editorSession.set({editing: false})}>
                    <Eye className="icon-ui-12" />
                    {compact ? null : t.editor.view}
                </Button>
            )}
        </>
    ) : null

    return (
        <div ref={measure} className="flex h-full min-h-0 min-w-0 flex-col bg-surface">
            {file ? (
                <PanelToolbar
                    label={showDiff && diff ? fmt(t.checkpoints.diff.title, {file: diff.file, parent: diff.parent}) : file}
                    actions={showDiff ? diffActions : actions}
                />
            ) : null}
            {showDiff && diff ? (
                <div
                    className={cn(
                        'shrink-0 border-b border-line bg-surface-2 px-3 py-1 font-mono text-ui-11 text-muted',
                        sideBySide ? 'grid grid-cols-2 gap-3' : 'flex flex-wrap gap-x-4',
                    )}
                >
                    <span className="truncate" title={diff.parent}>
                        {fmt(t.checkpoints.diff.parentSide, {parent: diff.parent})}
                    </span>
                    <span className="truncate" title={diff.file}>
                        {fmt(t.checkpoints.diff.thisSide, {file: diff.file})}
                    </span>
                </div>
            ) : null}
            <div className="min-h-0 min-w-0 flex-1">
            {showDiff && diff ? (
                <EditorBoundary
                    resetKey={`${diff.file}:${diff.parent}`}
                    fallback={(err) => (
                        <p className="px-3 py-2 text-ui-12 text-danger">{fmt(t.editor.loadError, {msg: err})}</p>
                    )}
                >
                    <Suspense
                        fallback={
                            <div className="flex h-full items-center justify-center gap-2 text-ui-12 text-muted">
                                <Loader2 className="icon-ui-16 animate-spin text-accent" />
                                {t.checkpoints.diff.loading}
                            </div>
                        }
                    >
                        <DiffViewer
                            docKey={`${workspaceId ?? ''}:${diff.file}:${diff.parent}`}
                            original={diff.parentSource}
                            modified={diff.source}
                            sideBySide={sideBySide}
                            fontSize={settings.editorFontSize || 13}
                            tabSize={settings.editorIndent === 2 ? 2 : 4}
                        />
                    </Suspense>
                </EditorBoundary>
            ) : file ? (
                <EditorBoundary
                    resetKey={docKey}
                    fallback={(err) => (
                        <div className="flex h-full flex-col">
                            <p className="border-b border-line px-3 py-2 text-ui-12 text-danger">{fmt(t.editor.loadError, {msg: err})}</p>
                            <pre className="selectable min-h-0 flex-1 overflow-auto p-3 font-mono text-ui-12 leading-[1.55] text-fg">{source}</pre>
                        </div>
                    )}
                >
                <Suspense
                    fallback={
                        <div className="flex h-full items-center justify-center gap-2 text-ui-12 text-muted">
                            <Loader2 className="icon-ui-16 animate-spin text-accent" />
                            {t.editor.loading}
                        </div>
                    }
                >
                    <CodeEditor
                        docKey={docKey}
                        value={value}
                        readOnly={!editing}
                        fontSize={settings.editorFontSize || 13}
                        tabSize={settings.editorIndent === 2 ? 2 : 4}
                        wordWrap={settings.editorWordWrap}
                        minimap={settings.editorMinimap}
                        markers={markers}
                        onChange={onChange}
                        onRequestEdit={startEditing}
                        onFormat={() => void format()}
                        shot={shot?.mode === 'edit' || shot?.mode === 'search' ? shot.mode : undefined}
                    />
                </Suspense>
                </EditorBoundary>
            ) : (
                <div className="flex h-full items-center">
                    <Placeholder text={t.panes.editorBody} />
                </div>
            )}
            </div>
            {/* En el body y no dentro del panel: ⌘S funciona aunque el panel de código
                esté oculto, y un <dialog> dentro de un elemento aparcado no se vería. */}
            {createPortal(
                <SaveCheckpointDialog
                    open={saveOpen}
                    parent={file ?? ''}
                    saving={saving}
                    initial={shot?.mode === 'save' ? {label: shot.label ?? '', description: shot.description ?? ''} : undefined}
                    onCancel={() => setSaveOpen(false)}
                    onSave={(l, d) => void save(l, d)}
                />,
                document.body,
            )}
        </div>
    )
}
