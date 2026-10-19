import {Suspense, useCallback, useEffect, useRef, useState} from 'react'
import {CheckCircle2, CircleAlert, FilePlus2, FileText, Loader2, Lock, Pencil, Trash2, TriangleAlert} from 'lucide-react'
import type {main, skills} from '../../../wailsjs/go/models'
import {
    CreateSkillFile,
    DeleteSkillFile,
    ListSkillFiles,
    ReadSkillFile,
    RenameSkillFile,
    SaveSkillFile,
    ValidateSkillMarkdown,
} from '../../../wailsjs/go/main/App'
import {Button, IconButton} from '../ui/Button'
import {confirmDialog, Dialog} from '../ui/Dialog'
import {Input} from '../ui/Field'
import {toast} from '../ui/Toast'
import {SkillCodeEditor} from '../editor/lazy'
import type {SkillLanguage} from '../editor/SkillCodeEditor'
import {EditorBoundary} from '../editor/EditorBoundary'
import {errorText} from '../../lib/errors'
import {isMod, withKeys} from '../../lib/platform'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

const MAIN_FILE = 'SKILL.md'
// Espera desde la última tecla hasta revisar el frontmatter.
const VALIDATE_DEBOUNCE_MS = 300

type Draft = {saved: string; text: string}

function languageOf(path: string): SkillLanguage {
    const p = path.toLowerCase()
    if (p.endsWith('.md') || p.endsWith('.markdown')) return 'markdown'
    if (p.endsWith('.scad')) return 'openscad'
    return 'plaintext'
}

/**
 * SkillEditor es el editor de una skill (Configuración → Skills → Editar / Ver):
 * la lista de sus archivos a la izquierda y Monaco a la derecha. Las skills
 * propias se editan; las incluidas se ven en solo lectura. Guarda con ⌘S / Ctrl+S,
 * avisa de los cambios sin guardar al cerrar y valida el frontmatter de SKILL.md
 * mientras se escribe (Go es el que valida, aquí solo se marca).
 */
export function SkillEditor({
    skill,
    readOnly,
    onClose,
    onChanged,
    fontSize,
    tabSize,
    wordWrap,
    shot,
}: {
    /** La skill abierta; null = el editor está cerrado. */
    skill: main.SkillView | null
    readOnly: boolean
    onClose: () => void
    /** Algo se guardó, creó, renombró o borró: la lista de skills tiene que volver a leerse. */
    onChanged: () => void
    fontSize: number
    tabSize: number
    wordWrap: boolean
    /** Solo el banco de capturas: deja un archivo seleccionado, con un problema de frontmatter. */
    shot?: {file?: string; invalid?: boolean}
}) {
    const t = useT()
    const k = t.skills.editor
    const open = skill != null
    const name = skill?.name ?? ''

    const [files, setFiles] = useState<skills.File[]>([])
    const [current, setCurrent] = useState(MAIN_FILE)
    const [drafts, setDrafts] = useState<Record<string, Draft>>({})
    const [diags, setDiags] = useState<skills.Diagnostic[]>([])
    const [saving, setSaving] = useState(false)
    const [creating, setCreating] = useState(false)
    const [newName, setNewName] = useState('')
    const [renaming, setRenaming] = useState<string | null>(null)
    const [renameTo, setRenameTo] = useState('')
    const [reveal, setReveal] = useState<{line: number; nonce: number} | null>(null)

    // Lo que los callbacks asíncronos y el atajo necesitan ver al día.
    const live = useRef({drafts, current, readOnly, saving, open, name})
    live.current = {drafts, current, readOnly, saving, open, name}

    const isDirty = (path: string) => {
        const d = drafts[path]
        return !!d && d.text !== d.saved
    }
    const dirtyPaths = Object.keys(drafts).filter(isDirty)

    // ── Carga ───────────────────────────────────────────────────────────────
    const reloadFiles = useCallback(async (skillName: string) => {
        const list = (await ListSkillFiles(skillName)) ?? []
        setFiles(list)
        return list
    }, [])

    const openFile = useCallback(
        async (path: string, list?: skills.File[]) => {
            const skillName = live.current.name
            const f = (list ?? files).find((x) => x.path === path)
            setCurrent(path)
            if (f && !f.text) return
            if (live.current.drafts[path]) return
            try {
                const text = await ReadSkillFile(skillName, path)
                setDrafts((d) => (d[path] ? d : {...d, [path]: {saved: text, text}}))
            } catch (e) {
                toast.error(fmt(k.loadError, {file: path, msg: errorText(e)}))
            }
        },
        [files, k],
    )

    useEffect(() => {
        if (!open) return
        setFiles([])
        setDrafts({})
        setDiags([])
        setCreating(false)
        setRenaming(null)
        setReveal(null)
        setCurrent(MAIN_FILE)
        let alive = true
        ;(async () => {
            try {
                const list = await reloadFiles(name)
                if (!alive) return
                const first = shot?.file && list.some((f) => f.path === shot.file) ? shot.file : list.some((f) => f.path === MAIN_FILE) ? MAIN_FILE : (list.find((f) => f.text)?.path ?? MAIN_FILE)
                setCurrent(first)
                const text = await ReadSkillFile(name, first)
                if (!alive) return
                // El banco de capturas fotografía un SKILL.md con la descripción vacía.
                const shown = shot?.invalid && first === MAIN_FILE ? text.replace(/^description:.*$/m, 'description:') : text
                setDrafts({[first]: {saved: text, text: shown}})
            } catch (e) {
                if (alive) toast.error(errorText(e))
            }
        })()
        return () => {
            alive = false
        }
        // Solo al abrir otra skill.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open, name])

    // ── Validación del frontmatter ─────────────────────────────────────────
    const mainText = drafts[MAIN_FILE]?.text
    useEffect(() => {
        if (!open || readOnly || mainText == null) return
        let alive = true
        const timer = window.setTimeout(() => {
            ValidateSkillMarkdown(name, mainText)
                .then((ds) => alive && setDiags(ds ?? []))
                .catch(() => alive && setDiags([]))
        }, VALIDATE_DEBOUNCE_MS)
        return () => {
            alive = false
            window.clearTimeout(timer)
        }
    }, [open, readOnly, name, mainText])

    const hasErrors = diags.some((d) => d.severity === 'error')

    // ── Guardar ─────────────────────────────────────────────────────────────
    const save = useCallback(async () => {
        const {drafts: ds, current: path, readOnly: ro, saving: busy, name: skillName} = live.current
        const d = ds[path]
        if (ro || busy || !d || d.text === d.saved) return
        setSaving(true)
        try {
            const r = await SaveSkillFile(skillName, path, d.text)
            if (path === MAIN_FILE) setDiags(r.diagnostics ?? [])
            if (!r.saved) {
                toast.error(t.skills.editor.blocked)
                return
            }
            setDrafts((prev) => (prev[path] ? {...prev, [path]: {...prev[path], saved: d.text}} : prev))
            if (r.syncError) toast.error(fmt(t.skills.editor.syncFailed, {msg: r.syncError}))
            else toast.ok(fmt(t.skills.editor.saved, {file: path}))
            onChanged()
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setSaving(false)
        }
    }, [onChanged, t])

    // ⌘S / Ctrl+S desde cualquier punto del diálogo, en captura: antes de que
    // el atajo de la ventana principal (guardar checkpoint) lo vea.
    useEffect(() => {
        if (!open) return
        const onKey = (e: KeyboardEvent) => {
            if (isMod(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 's') {
                e.preventDefault()
                e.stopPropagation()
                void save()
            }
        }
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [open, save])

    // ── Cerrar ──────────────────────────────────────────────────────────────
    const requestClose = useCallback(async () => {
        if (!live.current.open) return // el cierre del propio <dialog> vuelve a llamar acá
        const dirty = Object.keys(live.current.drafts).filter((p) => live.current.drafts[p].text !== live.current.drafts[p].saved)
        if (dirty.length > 0) {
            const text = dirty.length === 1 ? fmt(k.discardOne, {file: dirty[0]}) : fmt(k.discardMany, {n: dirty.length})
            if (!(await confirmDialog(text, k.discard, k.keepEditing, {danger: true}))) return
        }
        onClose()
    }, [k, onClose])

    // ── Archivos ────────────────────────────────────────────────────────────
    const createFile = async () => {
        const rel = newName.trim()
        if (!rel) return
        try {
            await CreateSkillFile(name, rel)
            await reloadFiles(name)
            setCreating(false)
            setNewName('')
            setDrafts((d) => ({...d, [rel]: {saved: '', text: ''}}))
            setCurrent(rel)
            onChanged()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const renameFile = async (from: string) => {
        const to = renameTo.trim()
        if (!to || to === from) {
            setRenaming(null)
            return
        }
        try {
            const syncErr = await RenameSkillFile(name, from, to)
            setDrafts((d) => {
                if (!d[from]) return d
                const {[from]: moved, ...rest} = d
                return {...rest, [to]: moved}
            })
            if (live.current.current === from) setCurrent(to)
            await reloadFiles(name)
            setRenaming(null)
            if (syncErr) toast.error(fmt(k.syncFailed, {msg: syncErr}))
            onChanged()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const deleteFile = async (path: string) => {
        if (!(await confirmDialog(fmt(k.fileConfirm, {file: path, name}), k.fileDeleteOk, k.confirmCancel, {danger: true}))) return
        try {
            await DeleteSkillFile(name, path)
            setDrafts((d) => {
                const {[path]: _gone, ...rest} = d
                return rest
            })
            const list = await reloadFiles(name)
            if (live.current.current === path) await openFile(MAIN_FILE, list)
            toast.ok(fmt(k.fileDeleted, {file: path}))
            onChanged()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const draft = drafts[current]
    const currentFile = files.find((f) => f.path === current)
    const markers = current === MAIN_FILE && !readOnly ? diags.filter((d) => d.severity === 'error' || d.severity === 'warning').map((d) => ({line: d.line, severity: d.severity as 'error' | 'warning', message: d.message, code: d.code})) : []
    const dirty = isDirty(current)

    return (
        <Dialog
            open={open}
            onClose={() => void requestClose()}
            onCancel={() => void requestClose()}
            title={fmt(readOnly ? k.titleView : k.titleEdit, {name})}
            closeLabel={k.closeHint}
            className="w-[1120px]"
            footer={
                <>
                    <span className="selectable mr-auto min-w-0 flex-1 truncate self-center font-mono text-ui-11 text-faint" title={skill?.dir}>
                        {readOnly ? k.readOnlyNote : fmt(k.folder, {dir: skill?.dir ?? ''})}
                    </span>
                    <Button title={k.closeHint} onClick={() => void requestClose()}>
                        {k.close}
                    </Button>
                    {readOnly ? null : (
                        <Button
                            variant="primary"
                            title={current === MAIN_FILE && hasErrors ? k.blocked : withKeys(fmt(k.saveHint, {file: current}), 'mod+s')}
                            disabled={!dirty || saving || (current === MAIN_FILE && hasErrors)}
                            onClick={() => void save()}
                        >
                            {saving ? <Loader2 className="icon-ui-14 animate-spin" /> : null}
                            {k.save}
                        </Button>
                    )}
                </>
            }
        >
            <div className="flex h-[66vh] min-h-[420px]">
                {/* Archivos */}
                <div className="flex w-[268px] shrink-0 flex-col border-r border-line">
                    <div className="flex h-9 shrink-0 items-center justify-between border-b border-line pr-1.5 pl-3">
                        <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.files}</span>
                        {readOnly ? (
                            <span className="flex items-center gap-1 pr-1.5 text-ui-11 text-faint" title={k.readOnlyNote}>
                                <Lock className="icon-ui-12" />
                                {k.readOnly}
                            </span>
                        ) : (
                            <IconButton title={k.newFileHint} className="size-6" onClick={() => setCreating(true)}>
                                <FilePlus2 className="icon-ui-14" />
                            </IconButton>
                        )}
                    </div>
                    <ul className="min-h-0 flex-1 overflow-auto p-1.5" role="listbox" aria-label={k.files}>
                        {creating ? (
                            <li className="mb-1.5">
                                <Input
                                    data-autofocus
                                    className="h-7 font-mono text-ui-11-5"
                                    value={newName}
                                    placeholder={k.newFilePlaceholder}
                                    title={k.newFileHint}
                                    autoFocus
                                    onChange={(e) => setNewName(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') void createFile()
                                        if (e.key === 'Escape') {
                                            // Sin esto Escape también cierra el diálogo del editor.
                                            e.preventDefault()
                                            e.stopPropagation()
                                            setCreating(false)
                                            setNewName('')
                                        }
                                    }}
                                    onBlur={() => !newName.trim() && setCreating(false)}
                                />
                            </li>
                        ) : null}
                        {files.map((f) => {
                            const active = f.path === current
                            const isMainFile = f.path === MAIN_FILE
                            if (renaming === f.path) {
                                return (
                                    <li key={f.path} className="mb-1">
                                        <Input
                                            className="h-7 font-mono text-ui-11-5"
                                            value={renameTo}
                                            title={fmt(k.fileRenameHint, {file: f.path})}
                                            autoFocus
                                            onChange={(e) => setRenameTo(e.target.value)}
                                            onFocus={(e) => e.currentTarget.select()}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter') void renameFile(f.path)
                                                if (e.key === 'Escape') {
                                                    e.preventDefault()
                                                    e.stopPropagation()
                                                    setRenaming(null)
                                                }
                                            }}
                                            onBlur={() => setRenaming(null)}
                                        />
                                    </li>
                                )
                            }
                            return (
                                <li
                                    key={f.path}
                                    role="option"
                                    aria-selected={active}
                                    className={cn(
                                        'group flex items-center rounded-md transition-colors duration-100',
                                        active ? 'bg-accent-soft text-fg' : 'text-muted hover:bg-surface-2 hover:text-fg',
                                    )}
                                >
                                    <button
                                        type="button"
                                        disabled={!f.text}
                                        title={f.text ? fmt(k.openFileHint, {file: f.path}) : k.notText}
                                        onClick={() => void openFile(f.path)}
                                        className="flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md pl-2 text-left text-ui-12 disabled:cursor-not-allowed disabled:opacity-50"
                                    >
                                        <FileText className="icon-ui-14 shrink-0 text-faint" />
                                        <span className="truncate font-mono text-ui-11-5">{f.path}</span>
                                        {isDirty(f.path) ? <span className="size-1.5 shrink-0 rounded-full bg-accent" title={k.dirtyHint} aria-label={k.dirtyHint} /> : null}
                                        {isMainFile && hasErrors ? <CircleAlert className="icon-ui-12 shrink-0 text-danger" aria-label={k.errorsBadge} /> : null}
                                    </button>
                                    {!readOnly && !isMainFile ? (
                                        <span className="flex shrink-0 pr-0.5 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">
                                            <IconButton
                                                title={fmt(k.fileRenameHint, {file: f.path})}
                                                className="size-6"
                                                onClick={() => {
                                                    setRenaming(f.path)
                                                    setRenameTo(f.path)
                                                }}
                                            >
                                                <Pencil className="icon-ui-12" />
                                            </IconButton>
                                            <IconButton title={fmt(k.fileDeleteHint, {file: f.path})} className="size-6 hover:text-danger" onClick={() => void deleteFile(f.path)}>
                                                <Trash2 className="icon-ui-12" />
                                            </IconButton>
                                        </span>
                                    ) : null}
                                </li>
                            )
                        })}
                    </ul>
                </div>

                {/* Editor */}
                <div className="flex min-w-0 flex-1 flex-col">
                    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-line px-3">
                        <span className="truncate font-mono text-ui-11-5 text-fg">{current}</span>
                        {dirty ? <span className="size-1.5 shrink-0 rounded-full bg-accent" title={k.dirtyHint} aria-label={k.dirtyHint} /> : null}
                        {currentFile ? <span className="ml-auto shrink-0 font-mono text-ui-10-5 text-faint">{languageOf(current)}</span> : null}
                    </div>
                    <div className="min-h-0 flex-1">
                        {draft ? (
                            <EditorBoundary resetKey={`${name}:${current}`} fallback={(err) => <p className="px-3 py-2 text-ui-12 text-danger">{fmt(k.loadError, {file: current, msg: err})}</p>}>
                                <Suspense
                                    fallback={
                                        <div className="flex h-full items-center justify-center gap-2 text-ui-12 text-muted">
                                            <Loader2 className="icon-ui-16 animate-spin text-accent" />
                                            {k.loading}
                                        </div>
                                    }
                                >
                                    <SkillCodeEditor
                                        docKey={`${name}:${current}`}
                                        value={draft.text}
                                        language={languageOf(current)}
                                        readOnly={readOnly}
                                        fontSize={fontSize}
                                        tabSize={tabSize}
                                        wordWrap={languageOf(current) === 'openscad' ? wordWrap : true}
                                        markers={markers}
                                        reveal={reveal}
                                        onChange={(text) => setDrafts((d) => (d[live.current.current] ? {...d, [live.current.current]: {...d[live.current.current], text}} : d))}
                                    />
                                </Suspense>
                            </EditorBoundary>
                        ) : (
                            <div className="flex h-full items-center justify-center gap-2 text-ui-12 text-muted">
                                <Loader2 className="icon-ui-16 animate-spin text-accent" />
                                {k.loading}
                            </div>
                        )}
                    </div>
                    {/* Frontmatter de SKILL.md: lo que impide guardar y lo que solo conviene mirar */}
                    {!readOnly && current === MAIN_FILE ? (
                        <div className="max-h-[132px] shrink-0 overflow-auto border-t border-line bg-surface-2 px-3 py-2" aria-live="polite">
                            <div className="mb-1 font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.problems}</div>
                            {diags.length === 0 ? (
                                <p className="flex items-center gap-1.5 text-ui-11-5 text-ok">
                                    <CheckCircle2 className="icon-ui-14 shrink-0" />
                                    {k.problemsOk}
                                </p>
                            ) : (
                                <ul className="flex flex-col gap-0.5">
                                    {diags.map((d, i) => (
                                        <li key={`${d.code}-${i}`}>
                                            <button
                                                type="button"
                                                title={fmt(k.problemHint, {n: d.line})}
                                                onClick={() => setReveal({line: d.line, nonce: Date.now()})}
                                                className="flex w-full items-start gap-2 rounded px-1 py-0.5 text-left text-ui-11-5 hover:bg-surface"
                                            >
                                                {d.severity === 'error' ? <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" /> : <TriangleAlert className="mt-px icon-ui-14 shrink-0 text-warn" />}
                                                <span className="shrink-0 font-mono text-faint">{fmt(k.problemLine, {n: d.line})}</span>
                                                <span className="text-fg">{d.message}</span>
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </div>
                    ) : null}
                </div>
            </div>
        </Dialog>
    )
}
