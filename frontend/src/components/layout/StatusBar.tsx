import {Box, Cpu, CircleAlert, FolderClosed, Info, Lock, PencilLine, Printer, TriangleAlert} from 'lucide-react'
import {printStore} from '../../lib/printSession'
import {overflowAxes, plateSize} from '../viewer/PlateAlert'
import type {render} from '../../../wailsjs/go/models'
import {editorSession} from '../../lib/editorSession'
import {counts} from '../../lib/problems'
import {useProblems} from '../../hooks/useProblems'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {wasmShortVersion} from '../../lib/engine'
import {colorsCount} from '../../lib/printerInfo'

function Item({title, children, className}: {title: string; children: React.ReactNode; className?: string}) {
    return (
        <span className={cn('flex shrink-0 items-center gap-1.5', className)} title={title}>
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
            {editing ? <PencilLine className="icon-ui-12" /> : <Lock className="icon-ui-12" />}
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
    colors,
    file,
    renderDiags,
    openscad,
    onProblems,
    onPrint,
    onPrinter,
    onOpenSettings,
}: {
    workspace?: string
    printer?: string
    /** Los colores del workspace (S14): «<Impresora> · N colores». */
    colors?: number
    file: string | null
    renderDiags: render.Diagnostic[] | undefined
    openscad: {version: string; missing: boolean; engine?: string} | null
    onProblems: () => void
    onPrint: () => void
    /** Abre la configuración del workspace (clic en la impresora). */
    onPrinter?: () => void
    /** Abre Configuración → OpenSCAD (la insignia del motor integrado lleva ahí). */
    onOpenSettings: () => void
}) {
    const t = useT()
    const plate = printStore.use((s) => s.plate)
    const problems = useProblems(file ? renderDiags : undefined)
    const c = counts(file ? problems : [])
    return (
        <footer data-lock className="flex h-6 min-w-0 shrink-0 items-center gap-4 overflow-clip border-t border-line bg-surface px-3 font-mono text-ui-10-5 whitespace-nowrap text-muted">
            {workspace ? (
                <Item title={t.status.workspaceHint} className="min-w-0 shrink">
                    <FolderClosed className="icon-ui-12 shrink-0 text-faint" />
                    <span className="truncate">{workspace}</span>
                </Item>
            ) : (
                <span className="text-faint">{t.status.noWorkspace}</span>
            )}
            {printer ? (
                onPrinter ? (
                    <button
                        type="button"
                        onClick={onPrinter}
                        title={t.status.printerHint}
                        data-testid="status-printer"
                        className="flex min-w-0 shrink items-center gap-1.5 rounded px-1 transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
                    >
                        <Printer className="icon-ui-12 shrink-0 text-faint" />
                        <span className="truncate">{colors ? `${printer} · ${colorsCount(t, colors)}` : printer}</span>
                    </button>
                ) : (
                    <Item title={t.status.printerHint} className="min-w-0 shrink">
                        <Printer className="icon-ui-12 shrink-0 text-faint" />
                        <span className="truncate">{printer}</span>
                    </Item>
                )
            ) : null}
            <div className="min-w-0 flex-1" />
            {file && plate && !plate.fits ? (
                <button
                    type="button"
                    onClick={onPrint}
                    title={fmt(t.plate.statusHint, {axes: overflowAxes(plate), plate: plateSize(plate)})}
                    className="flex min-w-0 shrink items-center gap-1 rounded px-1 text-danger transition-colors duration-100 hover:bg-surface-2"
                >
                    <TriangleAlert className="icon-ui-12 shrink-0" />
                    <span className="truncate">{fmt(t.plate.status, {axes: overflowAxes(plate)})}</span>
                </button>
            ) : null}
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
                className="flex shrink-0 items-center gap-2 rounded px-1 transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
            >
                <span className={cn('flex items-center gap-1', c.error > 0 && 'text-danger')}>
                    <CircleAlert className="icon-ui-12" />
                    {c.error}
                </span>
                <span className={cn('flex items-center gap-1', c.warning > 0 && 'text-warn')}>
                    <TriangleAlert className="icon-ui-12" />
                    {c.warning}
                </span>
                <span className={cn('flex items-center gap-1', c.info > 0 && 'text-info')}>
                    <Info className="icon-ui-12" />
                    {c.info}
                </span>
            </button>
            {openscad && !openscad.missing && openscad.engine === 'wasm' ? (
                <button
                    type="button"
                    onClick={onOpenSettings}
                    title={fmt(t.status.wasmHint, {v: openscad.version})}
                    className="flex shrink-0 items-center gap-1 rounded px-1 text-info transition-colors duration-100 hover:bg-surface-2"
                >
                    <Cpu className="icon-ui-12 shrink-0" />
                    {fmt(t.status.wasm, {v: wasmShortVersion(openscad.version)})}
                </button>
            ) : openscad ? (
                <Item title={openscad.missing ? t.status.openscadMissingHint : t.status.openscadHint} className={openscad.missing ? 'text-warn' : undefined}>
                    <Box className="icon-ui-12 shrink-0" />
                    {openscad.missing ? t.status.openscadMissing : fmt(t.status.openscad, {v: openscad.version})}
                </Item>
            ) : null}
        </footer>
    )
}
