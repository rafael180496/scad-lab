import {Box, CircleAlert, FolderClosed, Info, Lock, PencilLine, Printer, TriangleAlert} from 'lucide-react'
import type {render} from '../../../wailsjs/go/models'
import {editorSession} from '../../lib/editorSession'
import {counts} from '../../lib/problems'
import {useProblems} from '../../hooks/useProblems'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

function Item({title, children, className}: {title: string; children: React.ReactNode; className?: string}) {
    return (
        <span className={cn('flex min-w-0 items-center gap-1.5', className)} title={title}>
            {children}
        </span>
    )
}

// Cursor del editor: se suscribe solo, así moverlo no redibuja la ventana.
function Cursor() {
    const t = useT()
    const line = editorSession.use((s) => s.line)
    const col = editorSession.use((s) => s.col)
    return (
        <Item title={t.status.cursorHint}>
            <span className="tabular-nums">{fmt(t.status.cursor, {line, col})}</span>
        </Item>
    )
}

function Mode() {
    const t = useT()
    const editing = editorSession.use((s) => s.editing)
    const dirty = editorSession.use((s) => s.dirty)
    return (
        <Item title={editing ? t.editor.editingHint : t.editor.readOnlyHint} className={editing ? 'text-accent' : undefined}>
            {editing ? <PencilLine className="size-3" /> : <Lock className="size-3" />}
            {editing ? t.editor.editing : t.editor.readOnly}
            {dirty ? <span className="text-accent">· {t.editor.unsaved}</span> : null}
        </Item>
    )
}

// StatusBar es la barra inferior: dónde se está (workspace, impresora), el
// editor (cursor, modo) y el estado (problemas, OpenSCAD).
export function StatusBar({
    workspace,
    printer,
    file,
    renderDiags,
    openscad,
    onProblems,
}: {
    workspace?: string
    printer?: string
    file: string | null
    renderDiags: render.Diagnostic[] | undefined
    openscad: {version: string; missing: boolean} | null
    onProblems: () => void
}) {
    const t = useT()
    const problems = useProblems(file ? renderDiags : undefined)
    const c = counts(file ? problems : [])
    return (
        <footer className="flex h-6 shrink-0 items-center gap-4 border-t border-line bg-surface px-3 font-mono text-[10.5px] text-muted">
            {workspace ? (
                <Item title={t.status.workspaceHint}>
                    <FolderClosed className="size-3 shrink-0 text-faint" />
                    <span className="truncate">{workspace}</span>
                </Item>
            ) : (
                <span className="text-faint">{t.status.noWorkspace}</span>
            )}
            {printer ? (
                <Item title={t.status.printerHint}>
                    <Printer className="size-3 shrink-0 text-faint" />
                    <span className="truncate">{printer}</span>
                </Item>
            ) : null}
            <div className="flex-1" />
            {file ? (
                <>
                    <Mode />
                    <Cursor />
                </>
            ) : null}
            <button
                type="button"
                onClick={onProblems}
                title={fmt(t.status.problemsHint, {e: c.error, w: c.warning, i: c.info})}
                className="flex items-center gap-2 rounded px-1 transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
            >
                <span className={cn('flex items-center gap-1', c.error > 0 && 'text-danger')}>
                    <CircleAlert className="size-3" />
                    {c.error}
                </span>
                <span className={cn('flex items-center gap-1', c.warning > 0 && 'text-warn')}>
                    <TriangleAlert className="size-3" />
                    {c.warning}
                </span>
                <span className={cn('flex items-center gap-1', c.info > 0 && 'text-info')}>
                    <Info className="size-3" />
                    {c.info}
                </span>
            </button>
            {openscad ? (
                <Item title={openscad.missing ? t.status.openscadMissingHint : t.status.openscadHint} className={openscad.missing ? 'text-warn' : undefined}>
                    <Box className="size-3 shrink-0" />
                    {openscad.missing ? t.status.openscadMissing : fmt(t.status.openscad, {v: openscad.version})}
                </Item>
            ) : null}
        </footer>
    )
}
