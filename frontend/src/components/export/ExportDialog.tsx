import {useCallback, useEffect, useState} from 'react'
import {Box, Download, ExternalLink, FileArchive, Loader2, RefreshCw, Settings, TriangleAlert} from 'lucide-react'
import type {exporter} from '../../../wailsjs/go/models'
import {ExportCheckpoint, PreviewExportParts} from '../../../wailsjs/go/main/App'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {toast} from '../ui/Toast'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {errorText} from '../../lib/errors'

type Format = '3mf' | 'stl-per-color'

// Diálogo de exportación 3MF (S9): muestra qué va a salir —una parte por color,
// con el extrusor que le toca— y los avisos, antes de elegir dónde guardar. La
// vista previa viene de PreviewExportParts (OpenSCAD renderiza el modelo con
// colores la primera vez; después es caché).
export function ExportDialog({
    open,
    onClose,
    workspaceId,
    fileName,
    bambuFound,
    onExported,
    onOpenSettings,
}: {
    open: boolean
    onClose: () => void
    workspaceId: number | null
    fileName: string | null
    /** Hay Bambu Studio detectado: se ofrece «Exportar y abrir». */
    bambuFound: boolean
    /** Ya se guardó: `open` = el usuario pidió abrirlo en Bambu Studio. */
    onExported: (path: string, open: boolean) => void
    onOpenSettings: () => void
}) {
    const t = useT()
    const [preview, setPreview] = useState<exporter.Preview | null>(null)
    const [error, setError] = useState('')
    const [loading, setLoading] = useState(false)
    const [format, setFormat] = useState<Format>('3mf')
    const [busy, setBusy] = useState(false)
    // `nonce` fuerza una carga nueva (Reintentar) sin cambiar de checkpoint.
    const [nonce, setNonce] = useState(0)

    useEffect(() => {
        if (!open || !workspaceId || !fileName) return
        let alive = true
        setLoading(true)
        setError('')
        setPreview(null)
        PreviewExportParts(workspaceId, fileName)
            .then((p) => alive && setPreview(p))
            .catch((e) => alive && setError(errorText(e)))
            .finally(() => alive && setLoading(false))
        return () => {
            alive = false
        }
    }, [open, workspaceId, fileName, nonce])

    // Cada apertura vuelve al formato de Bambu: el otro es la excepción.
    useEffect(() => {
        if (open) setFormat('3mf')
    }, [open])

    const run = useCallback(
        async (andOpen: boolean) => {
            if (!workspaceId || !fileName) return
            setBusy(true)
            try {
                const res = await ExportCheckpoint(workspaceId, fileName, format)
                // Path vacío = canceló el diálogo de guardar: el de exportación sigue abierto.
                if (res.path) {
                    onClose()
                    onExported(res.path, andOpen)
                }
            } catch (e) {
                toast.error(errorText(e))
            } finally {
                setBusy(false)
            }
        },
        [workspaceId, fileName, format, onClose, onExported],
    )

    const parts = preview?.parts ?? []
    const warnings = preview?.warnings ?? []
    const triangles = parts.reduce((n, p) => n + p.triangles, 0)
    const num = (n: number) => n.toLocaleString(locale())
    const ready = !!preview && !loading && parts.length > 0
    const perColor = format === 'stl-per-color'

    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={t.export.title}
            closeLabel={t.export.close}
            className="w-[640px]"
            footer={
                <>
                    <Button title={t.export.cancel} onClick={onClose}>
                        {t.export.cancel}
                    </Button>
                    <Button variant={bambuFound && !perColor ? 'outline' : 'primary'} title={t.export.exportHint} disabled={!ready || busy} onClick={() => void run(false)}>
                        {busy ? <Loader2 className="icon-ui-14 animate-spin" /> : <Download className="icon-ui-14" />}
                        {busy ? t.export.exporting : t.export.export}
                    </Button>
                    {bambuFound && !perColor ? (
                        <Button variant="primary" title={t.export.exportOpenHint} disabled={!ready || busy} onClick={() => void run(true)}>
                            <ExternalLink className="icon-ui-14" />
                            {t.export.exportOpen}
                        </Button>
                    ) : null}
                </>
            }
        >
            <div className="flex flex-col gap-4 px-4 py-4">
                <p className="text-ui-12-5 leading-relaxed text-muted">
                    {fmt(t.export.intro, {file: fileName ?? ''})}
                </p>

                <div className="flex flex-col gap-1.5">
                    <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.export.format}</span>
                    <div role="radiogroup" aria-label={t.export.format} className="grid grid-cols-2 gap-2">
                        <FormatCard
                            checked={format === '3mf'}
                            icon={<Box className="icon-ui-16" />}
                            label={t.export.format3mf}
                            desc={t.export.format3mfDesc}
                            onSelect={() => setFormat('3mf')}
                        />
                        <FormatCard
                            checked={perColor}
                            icon={<FileArchive className="icon-ui-16" />}
                            label={t.export.formatZip}
                            desc={t.export.formatZipDesc}
                            onSelect={() => setFormat('stl-per-color')}
                        />
                    </div>
                </div>

                <div className="flex flex-col gap-1.5">
                    <div className="flex items-baseline justify-between gap-3">
                        <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.export.parts}</span>
                        {ready ? (
                            <span className="font-mono text-ui-10-5 text-faint">
                                {parts.length === 1
                                    ? fmt(t.export.summaryOne, {tri: num(triangles)})
                                    : fmt(t.export.summaryMany, {n: parts.length, tri: num(triangles)})}
                            </span>
                        ) : null}
                    </div>

                    {loading ? (
                        <div className="flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-4 text-ui-12 text-muted">
                            <Loader2 className="icon-ui-16 shrink-0 animate-spin text-accent" />
                            {t.export.loading}
                        </div>
                    ) : null}

                    {error ? (
                        <div className="flex flex-col items-start gap-2 rounded-lg border border-danger/50 bg-surface-2 p-3">
                            <p className="selectable text-ui-12 leading-relaxed break-words">{fmt(t.export.loadError, {msg: error})}</p>
                            <Button title={t.export.retryHint} onClick={() => setNonce((n) => n + 1)}>
                                <RefreshCw className="icon-ui-14" />
                                {t.export.retry}
                            </Button>
                        </div>
                    ) : null}

                    {ready ? (
                        <div className="divide-y divide-line rounded-lg border border-line">
                            <div className="grid grid-cols-[minmax(0,1fr)_calc(96px*var(--ui-font-scale))_calc(72px*var(--ui-font-scale))] gap-3 px-3 py-1.5 font-mono text-ui-10-5 tracking-wider text-faint uppercase">
                                <span>{t.export.colPart}</span>
                                <span className="text-right">{t.export.colTriangles}</span>
                                <span className="text-right">{t.export.colExtruder}</span>
                            </div>
                            {parts.map((p) => (
                                <div key={`${p.extruder}-${p.color}`} className="grid grid-cols-[minmax(0,1fr)_calc(96px*var(--ui-font-scale))_calc(72px*var(--ui-font-scale))] items-center gap-3 px-3 py-2">
                                    <span className="flex min-w-0 items-center gap-2">
                                        <span
                                            className="icon-ui-14 shrink-0 rounded border border-line-strong"
                                            style={{backgroundColor: p.color}}
                                            title={p.color}
                                            aria-hidden
                                        />
                                        <span className="truncate text-ui-12-5 font-medium" title={p.name}>
                                            {p.name}
                                        </span>
                                        {p.uncolored ? (
                                            <>
                                                <span className="shrink-0 font-mono text-ui-10-5 text-faint">{p.color}</span>
                                                <span className="shrink-0 rounded bg-warn/15 px-1.5 py-px font-mono text-ui-10 text-warn">{t.export.uncolored}</span>
                                            </>
                                        ) : null}
                                    </span>
                                    <span className="text-right font-mono text-ui-11-5 text-muted">{num(p.triangles)}</span>
                                    <span
                                        className={cn('text-right font-mono text-ui-11-5', perColor ? 'text-faint' : 'text-fg')}
                                        title={perColor ? t.export.extruderNone : fmt(t.export.extruderHint, {n: p.extruder})}
                                    >
                                        {perColor ? '—' : fmt(t.export.extruderN, {n: p.extruder})}
                                    </span>
                                </div>
                            ))}
                        </div>
                    ) : null}
                </div>

                {ready ? (
                    <div className="flex flex-col gap-1.5">
                        <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.export.warnings}</span>
                        {warnings.length === 0 ? (
                            <p className="text-ui-12 text-faint">{t.export.noWarnings}</p>
                        ) : (
                            <ul className="selectable flex flex-col gap-1.5">
                                {warnings.map((w, i) => (
                                    <li key={i} className="flex items-start gap-2 rounded-lg border border-warn/50 bg-surface-2 px-3 py-2 text-ui-12 leading-snug">
                                        <TriangleAlert className="mt-px icon-ui-14 shrink-0 text-warn" />
                                        <span className="break-words">{w}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </div>
                ) : null}

                {!bambuFound && !perColor ? (
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-ui-11-5 leading-snug text-faint">
                        <span className="min-w-0 flex-1 basis-60">{t.export.noBambu}</span>
                        <Button
                            variant="ghost"
                            title={t.export.openSettingsHint}
                            onClick={() => {
                                onClose()
                                onOpenSettings()
                            }}
                        >
                            <Settings className="icon-ui-14" />
                            {t.export.openSettings}
                        </Button>
                    </div>
                ) : null}
            </div>
        </Dialog>
    )
}

function FormatCard({
    checked,
    icon,
    label,
    desc,
    onSelect,
}: {
    checked: boolean
    icon: React.ReactNode
    label: string
    desc: string
    onSelect: () => void
}) {
    return (
        <button
            type="button"
            role="radio"
            aria-checked={checked}
            title={desc}
            onClick={onSelect}
            className={cn(
                'no-drag flex flex-col items-start gap-1 rounded-lg border px-3 py-2.5 text-left transition-colors duration-150',
                checked ? 'border-accent bg-accent-soft' : 'border-line hover:bg-surface-2',
            )}
        >
            <span className={cn('flex items-center gap-1.5 text-ui-12-5 font-medium', checked ? 'text-fg' : 'text-muted')}>
                <span className={checked ? 'text-accent' : 'text-faint'}>{icon}</span>
                {label}
            </span>
            <span className="text-ui-11-5 leading-snug text-faint">{desc}</span>
        </button>
    )
}
