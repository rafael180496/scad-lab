import {Download, Square} from 'lucide-react'
import {Button} from '../ui/Button'
import {ProgressBar} from '../ui/ProgressBar'
import {useOpenSCADDownload} from '../../hooks/useOpenSCADDownload'
import {fmt, useT} from '../../i18n'
import {formatBytes} from '../../lib/bytes'
import {cn} from '../../lib/cn'

// DownloadControls es lo mismo en el aviso del panel de render y en Configuración
// → OpenSCAD: el botón «Descargar OpenSCAD» y, mientras corre, la fase, el avance
// y «Cancelar». No muestra nada si la plataforma no tiene descarga o ya hay una
// instalada (quien lo usa decide qué decir en ese caso).
export function DownloadControls({className}: {className?: string}) {
    const t = useT()
    const d = t.openscadDl
    const dl = useOpenSCADDownload()
    const info = dl.info

    if (dl.busy) {
        const known = dl.phase === 'download' && dl.total > 0
        const phase = dl.phase === 'verify' ? d.phaseVerify : dl.phase === 'extract' ? d.phaseExtract : d.phaseDownload
        const detail = known ? fmt(d.progress, {received: formatBytes(dl.received), total: formatBytes(dl.total), pct: Math.round((dl.received / dl.total) * 100)}) : ''
        return (
            <div className={cn('w-full min-w-[260px]', className)}>
                <div className="flex items-center gap-3">
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                        <div className="flex items-baseline justify-between gap-3 text-ui-11-5">
                            <span className="font-medium text-fg">{phase}</span>
                            <span className="font-mono text-ui-10-5 text-muted tabular-nums">{detail}</span>
                        </div>
                        <ProgressBar value={known ? dl.received / dl.total : null} label={phase} />
                    </div>
                    <Button title={d.cancelHint} onClick={dl.cancel}>
                        <Square className="icon-ui-12" />
                        {d.cancel}
                    </Button>
                </div>
            </div>
        )
    }
    if (!info || !info.supported || info.installed) return null
    return (
        <div className={className}>
            <Button
                variant="primary"
                title={fmt(d.downloadHint, {v: info.version, size: formatBytes(info.sizeBytes)})}
                disabled={dl.removing}
                onClick={dl.start}
            >
                <Download className="icon-ui-14" />
                {d.download}
                <span className="font-mono text-ui-10-5 opacity-80">{formatBytes(info.sizeBytes)}</span>
            </Button>
        </div>
    )
}
