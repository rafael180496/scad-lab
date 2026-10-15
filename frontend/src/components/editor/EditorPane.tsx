import {Suspense, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {Code2, Eye, Loader2, Lock, PencilLine, Save, Search, Undo2, WandSparkles} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {LintCheckpointSource, SaveAsCheckpoint} from '../../../wailsjs/go/main/App'
import {Pane, Placeholder} from '../layout/Pane'
import {Button, IconButton} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {CodeEditor} from './lazy'
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
export function EditorPane({
    workspaceId,
    file,
    source,
    renderDiags,
    settings,
    className,
    style,
    onSaved,
    shot,
}: {
    workspaceId: number | null
    file: string | null
    source: string
    renderDiags: render.Diagnostic[] | undefined
    settings: store.Settings
    className?: string
    style?: React.CSSProperties
    onSaved: (fileName: string) => void
    shot?: EditorShot
}) {
    const t = useT()
    const editing = editorSession.use((s) => s.editing)
    const dirty = editorSession.use((s) => s.dirty)
    const lint = editorSession.use((s) => s.lint)
    const [reset, setReset] = useState(0)
    const [saveOpen, setSaveOpen] = useState(false)
    const [saving, setSaving] = useState(false)
    const sourceRef = useRef(source)
    sourceRef.current = source
    const lintSeq = useRef(0)
    const lintTimer = useRef<number | undefined>(undefined)

    const docKey = `${workspaceId ?? ''}:${file ?? ''}:${reset}`

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
            editorSession.set({editing: false, dirty: false, lint: []})
        }
        if (!file) {
            lintSeq.current++
            return
        }
        if (!editorSession.get().editing) runLint(source)
    }, [docKey, source, file, runLint])
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

    const small = 'h-6 px-2 text-[11.5px]'
    const actions = file ? (
        <>
            <span
                className={cn(
                    'mr-1 flex shrink-0 items-center gap-1 rounded px-1.5 py-px text-[11px]',
                    editing ? 'bg-accent-soft text-accent' : 'text-faint',
                )}
                title={editing ? t.editor.editingHint : t.editor.readOnlyHint}
            >
                {editing ? <PencilLine className="size-3" /> : <Lock className="size-3" />}
                {editing ? t.editor.editing : t.editor.readOnly}
                {dirty ? <span className="size-1.5 rounded-full bg-accent" aria-label={t.editor.unsaved} /> : null}
            </span>
            <IconButton title={withKeys(t.editor.findHint, 'mod+f')} className="size-6" onClick={() => editorCommands()?.find(false)}>
                <Search className="size-3.5" />
            </IconButton>
            <IconButton title={withKeys(t.editor.formatHint, 'shift+alt+f')} className="size-6" onClick={() => void format()}>
                <WandSparkles className="size-3.5" />
            </IconButton>
            {!editing ? (
                <Button className={small} title={t.editor.editHint} onClick={startEditing}>
                    <PencilLine className="size-3" />
                    {t.editor.edit}
                </Button>
            ) : dirty ? (
                <>
                    <IconButton title={t.editor.discardHint} className="size-6" onClick={() => void discard()}>
                        <Undo2 className="size-3.5" />
                    </IconButton>
                    <Button variant="primary" className={small} title={withKeys(fmt(t.editor.saveHint, {file}), 'mod+s')} onClick={openSave}>
                        <Save className="size-3" />
                        {t.editor.save}
                    </Button>
                </>
            ) : (
                <Button className={small} title={t.editor.viewHint} onClick={() => editorSession.set({editing: false})}>
                    <Eye className="size-3" />
                    {t.editor.view}
                </Button>
            )}
        </>
    ) : null

    return (
        <Pane
            icon={<Code2 className="size-3.5" />}
            title={file ? '' : t.panes.editor}
            subtitle={file ?? undefined}
            actions={actions}
            className={className}
            style={style}
        >
            {file ? (
                <EditorBoundary
                    resetKey={docKey}
                    fallback={(err) => (
                        <div className="flex h-full flex-col">
                            <p className="border-b border-line px-3 py-2 text-[12px] text-danger">{fmt(t.editor.loadError, {msg: err})}</p>
                            <pre className="selectable min-h-0 flex-1 overflow-auto p-3 font-mono text-[12px] leading-[1.55] text-fg">{source}</pre>
                        </div>
                    )}
                >
                <Suspense
                    fallback={
                        <div className="flex h-full items-center justify-center gap-2 text-[12px] text-muted">
                            <Loader2 className="size-4 animate-spin text-accent" />
                            {t.editor.loading}
                        </div>
                    }
                >
                    <CodeEditor
                        docKey={docKey}
                        value={source}
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
            <SaveCheckpointDialog
                open={saveOpen}
                parent={file ?? ''}
                saving={saving}
                initial={shot?.mode === 'save' ? {label: shot.label ?? '', description: shot.description ?? ''} : undefined}
                onCancel={() => setSaveOpen(false)}
                onSave={(l, d) => void save(l, d)}
            />
        </Pane>
    )
}
