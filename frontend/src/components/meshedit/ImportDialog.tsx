import {useEffect, useState} from 'react'
import {CircleCheck, FileUp, Loader2, TriangleAlert} from 'lucide-react'
import {AnalyzeModelFile, ImportModel} from '../../../wailsjs/go/main/App'
import {main} from '../../../wailsjs/go/models'
import type {refmodel} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {num} from '../../lib/printFormat'
import {toast} from '../ui/Toast'
import {Button} from '../ui/Button'
import {Dialog} from '../ui/Dialog'
import {Field, Input} from '../ui/Field'
import {Row} from '../viewer/Row'
import type {BedVolume} from '../viewer/ViewPane'

// «Importar un modelo 3D» (S13): lo que se ve del archivo ANTES de traerlo al proyecto —medidas, triángulos, si la
// malla es cerrada, los colores de un 3MF— y los avisos que importan para editarlo. Importar copia el archivo a
// refs/ (el original no se toca), lo convierte a STL si es un 3MF u OBJ y crea el checkpoint base, que es una
// raíz nueva del árbol.

const BIG_TRIANGLES = 500_000

export const baseName = (path: string) => path.split(/[\\/]/).pop() ?? path

function warningsOf(t: ReturnType<typeof useT>, r: refmodel.Report, bed: BedVolume): string[] {
    const I = t.meshEdit.import
    const out: string[] = []
    if (!r.watertight) out.push(I.warnOpen)
    if (r.triangles > BIG_TRIANGLES) out.push(fmt(I.warnBig, {n: num(r.triangles, 0)}))
    if ((r.parts?.length ?? 0) > 1 && r.format !== 'stl') out.push(fmt(I.warnColors, {n: r.parts.length}))
    const [x, y, z] = r.size ?? [0, 0, 0]
    if (x > bed.x + 0.01 || y > bed.y + 0.01 || z > bed.z + 0.01) out.push(fmt(I.warnPlate, {size: [x, y, z].map((v) => num(v, 1)).join(' × '), plate: `${bed.x} × ${bed.y} × ${bed.z}`}))
    return out
}

export function ImportDialog({
    open,
    path,
    workspaceId,
    bed,
    onClose,
    onImported,
}: {
    open: boolean
    path: string
    workspaceId: number | null
    bed: BedVolume
    onClose: () => void
    /** Se importó: el checkpoint base nuevo. */
    onImported: (file: string) => void
}) {
    const t = useT()
    const I = t.meshEdit.import
    const [report, setReport] = useState<refmodel.Report | null>(null)
    const [error, setError] = useState('')
    const [center, setCenter] = useState(true)
    const [label, setLabel] = useState('')
    const [busy, setBusy] = useState(false)

    useEffect(() => {
        if (!open || !path) return
        let alive = true
        setReport(null)
        setError('')
        setCenter(true)
        setLabel('')
        AnalyzeModelFile(path)
            .then((r) => alive && setReport(r))
            .catch((e) => alive && setError(errorText(e)))
        return () => {
            alive = false
        }
    }, [open, path])

    const warnings = report ? warningsOf(t, report, bed) : []
    const importIt = async () => {
        if (workspaceId == null || !report || busy) return
        setBusy(true)
        try {
            const res = await ImportModel(workspaceId, path, main.ImportOptions.createFrom({centerOnBed: center, label: label.trim()}))
            toast.ok(fmt(I.done, {file: res.file}))
            onClose()
            onImported(res.file)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }

    return (
        <Dialog
            open={open}
            onClose={() => !busy && onClose()}
            title={I.title}
            closeLabel={I.cancel}
            className="w-[540px]"
            footer={
                <>
                    <Button title={I.cancel} onClick={onClose} disabled={busy}>
                        {I.cancel}
                    </Button>
                    <Button variant="primary" title={I.confirmHint} onClick={() => void importIt()} disabled={!report || busy} data-testid="import-confirm">
                        {busy ? <Loader2 className="icon-ui-14 animate-spin" /> : <FileUp className="icon-ui-14" />}
                        {I.confirm}
                    </Button>
                </>
            }
        >
            <form
                className="flex flex-col gap-4 px-4 py-4"
                onSubmit={(e) => {
                    e.preventDefault()
                    void importIt()
                }}
            >
                <div className="flex min-w-0 items-center gap-2">
                    <FileUp className="icon-ui-16 shrink-0 text-accent" />
                    <span className="selectable min-w-0 truncate font-mono text-ui-12-5 text-fg" title={path}>
                        {baseName(path)}
                    </span>
                    {report ? <span className="shrink-0 rounded border border-line px-1.5 font-mono text-ui-10-5 text-muted uppercase">{report.format}</span> : null}
                </div>

                {error ? <p className="selectable rounded-md border border-danger/50 px-3 py-2 text-ui-12 text-danger">{error}</p> : null}
                {!report && !error ? (
                    <p className="flex items-center gap-2 text-ui-12 text-muted">
                        <Loader2 className="icon-ui-14 animate-spin text-accent" />
                        {I.analyzing}
                    </p>
                ) : null}

                {report ? (
                    <>
                        <div className="flex flex-col gap-1 rounded-lg border border-line bg-surface-2 px-3 py-2" data-testid="import-report">
                            <Row label={t.viewer.size} hint={t.viewer.sizeHint}>
                                {fmt(t.viewer.mm, {v: (report.size ?? []).map((v) => num(v, 1)).join(' × ')})}
                            </Row>
                            <Row label={t.viewer.triangles} hint={t.viewer.trianglesHint}>
                                {num(report.triangles, 0)}
                            </Row>
                            <Row label={t.viewer.volume} hint={t.viewer.volumeHint}>
                                {fmt(t.viewer.cm3, {v: num(report.volumeMm3 / 1000, 2)})}
                            </Row>
                            <div className="pt-0.5">
                                {report.watertight ? (
                                    <span className="flex items-center gap-1 text-ui-11-5 text-ok" title={t.viewer.closedHint}>
                                        <CircleCheck className="icon-ui-12" />
                                        {t.viewer.closed}
                                    </span>
                                ) : (
                                    <span className="flex items-center gap-1 text-ui-11-5 text-warn" title={t.viewer.openHint}>
                                        <TriangleAlert className="icon-ui-12" />
                                        {t.viewer.open}
                                    </span>
                                )}
                            </div>
                        </div>

                        {(report.parts?.length ?? 0) > 1 ? (
                            <div className="flex flex-col gap-1">
                                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{I.colors}</span>
                                <ul className="flex flex-wrap gap-x-4 gap-y-1">
                                    {report.parts.map((p, i) => (
                                        <li key={i} className="flex items-center gap-1.5 text-ui-11-5 text-muted" title={p.name}>
                                            <span className="size-3 shrink-0 rounded-full border border-line-strong" style={{backgroundColor: p.color || undefined}} />
                                            {fmt(I.partTriangles, {name: p.color || p.name, n: num(p.triangles, 0)})}
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        ) : null}

                        {warnings.length > 0 ? (
                            <ul className="flex flex-col gap-1.5" data-testid="import-warnings">
                                {warnings.map((w, i) => (
                                    <li key={i} className="flex items-start gap-1.5 rounded-md border border-warn/50 px-2.5 py-1.5 text-ui-11-5 leading-snug text-warn">
                                        <TriangleAlert className="mt-px icon-ui-14 shrink-0" />
                                        <span className="min-w-0 text-fg">{w}</span>
                                    </li>
                                ))}
                            </ul>
                        ) : null}

                        <label className="flex cursor-pointer items-start gap-2 text-ui-12-5 text-fg" title={I.centerHint}>
                            <input type="checkbox" checked={center} onChange={(e) => setCenter(e.target.checked)} title={I.centerHint} className="mt-0.5 accent-[var(--c-accent)]" data-testid="import-center" />
                            <span>
                                {I.center}
                                <span className="block text-ui-11-5 text-faint">{I.centerHint}</span>
                            </span>
                        </label>

                        <Field label={I.label} hint={I.labelHint}>
                            <Input value={label} placeholder={baseName(path)} title={I.label} maxLength={80} onChange={(e) => setLabel(e.target.value)} />
                        </Field>
                    </>
                ) : null}
                <button type="submit" hidden title={I.confirm} />
            </form>
        </Dialog>
    )
}
