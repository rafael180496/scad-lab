import {Suspense, useCallback, useEffect, useRef, useState} from 'react'
import {Bot, FileDiff, FileClock, Loader2, RotateCcw, Undo2} from 'lucide-react'
import type {main, store} from '../../../wailsjs/go/models'
import {ListCheckpointHistory, ReadCheckpoint, ReadCheckpointVersion} from '../../../wailsjs/go/main/App'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {DiffViewer} from '../editor/lazy'
import {EditorBoundary} from '../editor/EditorBoundary'
import {absoluteTime, reasonKind, relativeTime, versionDate, versionLabel, type HistoryVersion} from '../../lib/history'
import {closeHistory, historyUI, openDiff, openHistory} from '../../lib/historyUI'
import {onEvent} from '../../lib/events'
import {errorText} from '../../lib/errors'
import {fmt, locale, useT, type Dict} from '../../i18n'
import {cn} from '../../lib/cn'
import {restoreVersion, versionWhen} from './historyActions'

// Diálogos del historial de un checkpoint (S16): la lista de versiones y la comparación de una
// versión con el archivo actual. Los abre lib/historyUI desde el menú del árbol y desde las
// cabeceras; HistoryHost se monta una vez en App.

// Recarga cuando el archivo cambia (la IA lo editó con el diálogo abierto, o se restauró una versión).
function useUpdateEpoch(workspaceId: number, file: string): number {
    const [epoch, setEpoch] = useState(0)
    useEffect(
        () =>
            onEvent('checkpoint:updated', (e) => {
                if (e.workspaceId === workspaceId && e.file === file) setEpoch((n) => n + 1)
            }),
        [workspaceId, file],
    )
    return epoch
}

function reasonText(t: Dict, reason: string): {text: string; hint: string} {
    switch (reasonKind(reason)) {
        case 'ai':
            return {text: t.history.dialog.reasonAi, hint: t.history.dialog.reasonAiHint}
        case 'restore':
            return {text: t.history.dialog.reasonRestore, hint: t.history.dialog.reasonRestoreHint}
        default:
            return {text: reason, hint: reason}
    }
}

function ReasonChip({reason}: {reason: string}) {
    const t = useT()
    const r = reasonText(t, reason)
    const ai = reasonKind(reason) === 'ai'
    return (
        <span
            title={r.hint}
            className={cn(
                'inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-px font-mono text-ui-10-5 tracking-[0.08em] uppercase',
                ai ? 'bg-accent-soft text-accent' : 'bg-surface-2 text-muted',
            )}
        >
            {ai ? <Bot className="icon-ui-12" /> : <RotateCcw className="icon-ui-12" />}
            {r.text}
        </span>
    )
}

function HistoryList({workspaceId, file}: {workspaceId: number; file: string}) {
    const t = useT()
    const h = t.history
    const [data, setData] = useState<main.CheckpointHistory | null>(null)
    const [error, setError] = useState('')
    const epoch = useUpdateEpoch(workspaceId, file)
    useEffect(() => {
        let alive = true
        ListCheckpointHistory(workspaceId, file)
            .then((d) => {
                if (!alive) return
                setData(d)
                setError('')
            })
            .catch((e) => alive && setError(errorText(e)))
        return () => {
            alive = false
        }
    }, [workspaceId, file, epoch])

    if (error) return <p className="px-4 py-4 text-ui-12-5 text-danger">{h.loadError}: {error}</p>
    if (!data)
        return (
            <div className="flex items-center justify-center gap-2 px-4 py-8 text-ui-12 text-muted">
                <Loader2 className="icon-ui-16 animate-spin text-accent" />
                {h.loading}
            </div>
        )
    const now = new Date()
    return (
        <div className="flex flex-col">
            <p className="px-4 pt-3 pb-2 text-ui-12 leading-relaxed text-muted">{fmt(h.dialog.intro, {file, max: data.max})}</p>
            {data.versions.length === 0 ? (
                <p className="mx-4 mb-4 rounded-lg border border-dashed border-line px-3 py-6 text-center text-ui-12 leading-relaxed text-faint">{h.dialog.empty}</p>
            ) : (
                <>
                    <p className="px-4 pb-1 font-mono text-ui-10-5 tracking-[0.12em] text-faint uppercase">{fmt(h.dialog.count, {n: data.versions.length})}</p>
                    <ul className="mx-4 mb-4 divide-y divide-line rounded-lg border border-line" aria-label={fmt(h.dialog.title, {file})}>
                        {data.versions.map((v) => {
                            const when = versionDate(v)
                            return (
                                <li key={v.id} className="flex items-start gap-3 px-3 py-2.5">
                                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <span className="text-ui-12-5 font-medium" title={versionWhen(v)}>
                                                {when ? relativeTime(when, now, locale()) : v.id}
                                            </span>
                                            <ReasonChip reason={v.reason} />
                                        </div>
                                        <p className={cn('text-ui-11-5 leading-snug break-words', v.note ? 'text-muted' : 'text-faint')}>{v.note || h.dialog.noNote}</p>
                                    </div>
                                    <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                                        <Button title={fmt(h.dialog.compareHint, {file})} onClick={() => openDiff(workspaceId, file, v.id)}>
                                            <FileDiff className="icon-ui-12" />
                                            {h.dialog.compare}
                                        </Button>
                                        <Button title={fmt(h.dialog.restoreHint, {file})} onClick={() => void restoreVersion(workspaceId, file, v, t)}>
                                            <RotateCcw className="icon-ui-12" />
                                            {h.dialog.restore}
                                        </Button>
                                    </div>
                                </li>
                            )
                        })}
                    </ul>
                </>
            )}
        </div>
    )
}

type Loaded = {version: HistoryVersion; before: string; after: string}

// Comparación de una versión con el archivo actual. Sin `versionId`, la más nueva guardada: lo que
// había justo antes del último cambio.
function DiffBody({workspaceId, file, versionId, settings, onLoaded}: {workspaceId: number; file: string; versionId?: string; settings: store.Settings; onLoaded: (v: HistoryVersion | null) => void}) {
    const t = useT()
    const h = t.history
    const [data, setData] = useState<Loaded | 'none' | null>(null)
    const [error, setError] = useState('')
    const epoch = useUpdateEpoch(workspaceId, file)
    const onLoadedRef = useRef(onLoaded)
    onLoadedRef.current = onLoaded
    useEffect(() => {
        let alive = true
        ;(async () => {
            const hist = await ListCheckpointHistory(workspaceId, file)
            const version = versionId ? hist.versions.find((v) => v.id === versionId) : hist.versions[0]
            if (!version) {
                if (alive) {
                    setData('none')
                    onLoadedRef.current(null)
                }
                return
            }
            const [before, after] = await Promise.all([ReadCheckpointVersion(workspaceId, file, version.id), ReadCheckpoint(workspaceId, file)])
            if (alive) {
                setData({version, before, after})
                setError('')
                onLoadedRef.current(version)
            }
        })().catch((e) => alive && setError(errorText(e)))
        return () => {
            alive = false
        }
    }, [workspaceId, file, versionId, epoch])

    if (error) return <p className="px-4 py-4 text-ui-12-5 text-danger">{h.diffDialog.loadError}: {error}</p>
    if (data === 'none') return <p className="px-4 py-8 text-center text-ui-12 text-faint">{h.diffDialog.none}</p>
    if (!data)
        return (
            <div className="flex items-center justify-center gap-2 px-4 py-8 text-ui-12 text-muted">
                <Loader2 className="icon-ui-16 animate-spin text-accent" />
                {h.diffDialog.loading}
            </div>
        )
    const when = versionDate(data.version)
    const before = fmt(h.diffDialog.before, {when: when ? absoluteTime(when, locale()) : data.version.id})
    const label = versionLabel({when: before, reason: reasonText(t, data.version.reason).text, note: data.version.note})
    return (
        <div className="flex h-[62vh] min-h-0 flex-col">
            <div className="grid shrink-0 grid-cols-2 gap-3 border-b border-line bg-surface-2 px-3 py-1 font-mono text-ui-11 text-muted">
                <span className="truncate" title={label}>
                    {label}
                </span>
                <span className="truncate" title={file}>
                    {h.diffDialog.current} · {file}
                </span>
            </div>
            <div className="min-h-0 flex-1">
                <EditorBoundary resetKey={`${file}:${data.version.id}`} fallback={(err) => <p className="px-3 py-2 text-ui-12 text-danger">{fmt(t.editor.loadError, {msg: err})}</p>}>
                    <Suspense
                        fallback={
                            <div className="flex h-full items-center justify-center gap-2 text-ui-12 text-muted">
                                <Loader2 className="icon-ui-16 animate-spin text-accent" />
                                {h.diffDialog.loading}
                            </div>
                        }
                    >
                        <DiffViewer
                            docKey={`${workspaceId}:${file}:${data.version.id}:${epoch}`}
                            original={data.before}
                            modified={data.after}
                            sideBySide
                            fontSize={settings.editorFontSize || 13}
                            tabSize={settings.editorIndent === 2 ? 2 : 4}
                        />
                    </Suspense>
                </EditorBoundary>
            </div>
        </div>
    )
}

export function HistoryHost({settings}: {settings: store.Settings | null}) {
    const t = useT()
    const dialog = historyUI.use((s) => s.dialog)
    const [shown, setShown] = useState<HistoryVersion | null>(null)
    const onLoaded = useCallback((v: HistoryVersion | null) => setShown(v), [])

    return (
        <>
            <Dialog
                open={dialog.kind === 'history'}
                onClose={closeHistory}
                title={dialog.kind === 'history' ? fmt(t.history.dialog.title, {file: dialog.file}) : ''}
                closeLabel={t.history.close}
                className="w-[680px]"
                footer={
                    <Button title={t.history.close} onClick={closeHistory}>
                        {t.history.close}
                    </Button>
                }
            >
                {dialog.kind === 'history' ? <HistoryList workspaceId={dialog.workspaceId} file={dialog.file} /> : null}
            </Dialog>
            <Dialog
                open={dialog.kind === 'diff' && settings != null}
                onClose={closeHistory}
                title={dialog.kind === 'diff' ? fmt(t.history.diffDialog.title, {file: dialog.file}) : ''}
                closeLabel={t.history.close}
                className="w-[min(1180px,94vw)]"
                footer={
                    dialog.kind === 'diff' ? (
                        <>
                            <Button title={fmt(t.history.diffDialog.backHint, {file: dialog.file})} onClick={() => openHistory(dialog.workspaceId, dialog.file)}>
                                <FileClock className="icon-ui-12" />
                                {t.history.diffDialog.back}
                            </Button>
                            <Button
                                title={fmt(t.history.dialog.restoreHint, {file: dialog.file})}
                                disabled={!shown}
                                onClick={() => {
                                    if (shown) void restoreVersion(dialog.workspaceId, dialog.file, shown, t).then((ok) => ok && closeHistory())
                                }}
                            >
                                <Undo2 className="icon-ui-12" />
                                {t.history.dialog.restore}
                            </Button>
                            <Button variant="primary" title={t.history.close} onClick={closeHistory}>
                                {t.history.close}
                            </Button>
                        </>
                    ) : undefined
                }
            >
                {dialog.kind === 'diff' && settings ? (
                    <DiffBody workspaceId={dialog.workspaceId} file={dialog.file} versionId={dialog.versionId} settings={settings} onLoaded={onLoaded} />
                ) : null}
            </Dialog>
        </>
    )
}
