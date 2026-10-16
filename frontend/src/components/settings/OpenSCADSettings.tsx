import {useEffect, useRef} from 'react'
import {Cpu, HardDriveDownload, Scale, Trash2} from 'lucide-react'
import type {detect} from '../../../wailsjs/go/models'
import {Button} from '../ui/Button'
import {DownloadControls} from '../render/DownloadControls'
import {fmt, useT} from '../../i18n'
import {formatBytes} from '../../lib/bytes'
import {wasmShortVersion} from '../../lib/engine'
import {useOpenSCADDownload} from '../../hooks/useOpenSCADDownload'

// Configuración → OpenSCAD, bloque de descarga y motor (S11): qué motor se está
// usando y por qué, el botón para descargar OpenSCAD (con progreso y Cancelar),
// quitar la descarga y el aviso de licencia del motor integrado.
// `tool` es la detección de OpenSCAD de la pestaña; `onChanged` la repite cuando
// termina una descarga o se quita (para que el estado de arriba no quede viejo).
export function OpenSCADSettings({tool, detected, onChanged}: {tool?: detect.Tool; detected: boolean; onChanged: () => void}) {
    const t = useT()
    const d = t.openscadDl
    const dl = useOpenSCADDownload()
    const info = dl.info

    const seen = useRef(dl.epoch)
    useEffect(() => {
        if (dl.epoch === seen.current) return
        seen.current = dl.epoch
        onChanged()
    }, [dl.epoch, onChanged])

    const source = tool?.source === 'custom' ? d.sourceCustom : tool?.source === 'managed' ? d.sourceManaged : d.sourceSystem
    const wasm = info ? wasmShortVersion(info.wasmVersion) : ''

    return (
        <section data-testid="openscad-download" className="flex flex-col gap-3 rounded-lg border border-line p-3">
            <div className="flex flex-col gap-1">
                <h3 className="flex items-center gap-1.5 text-ui-13 font-semibold">
                    <HardDriveDownload className="icon-ui-14 text-muted" />
                    {d.title}
                </h3>
                {info ? <p className="text-ui-11-5 leading-relaxed text-faint">{fmt(d.intro, {v: info.version, size: formatBytes(info.sizeBytes)})}</p> : null}
            </div>

            {detected ? (
                <p className="flex items-start gap-2 text-ui-12 leading-relaxed text-fg">
                    <Cpu className="mt-px icon-ui-14 shrink-0 text-muted" />
                    <span>
                        <span className="font-medium">{d.inUse}</span>{' '}
                        {tool?.found ? fmt(d.inUseNative, {source, v: tool.version || '—'}) : fmt(d.inUseWasm, {v: wasm})}
                    </span>
                </p>
            ) : null}

            {info?.installed ? (
                <>
                    <div className="flex items-center justify-between gap-3 rounded-md bg-surface-2 px-3 py-2">
                        <div className="flex min-w-0 flex-col gap-0.5">
                            <span className="text-ui-12 font-medium">{fmt(d.installed, {v: info.version})}</span>
                            <span className="selectable truncate font-mono text-ui-10-5 text-muted" title={info.installedPath}>
                                {info.installedPath}
                            </span>
                        </div>
                        <Button title={d.removeHint} onClick={dl.remove} disabled={dl.removing}>
                            <Trash2 className="icon-ui-14" />
                            {d.remove}
                        </Button>
                    </div>
                    {tool?.source === 'custom' ? <p className="text-ui-11-5 leading-relaxed text-warn">{d.customWins}</p> : null}
                </>
            ) : null}

            <DownloadControls />
            {info && !info.supported ? <p className="text-ui-11-5 leading-relaxed text-warn">{d.unsupported}</p> : null}

            <p className="flex items-start gap-2 border-t border-line pt-3 text-ui-11 leading-relaxed text-faint">
                <Scale className="mt-px icon-ui-12 shrink-0" />
                <span>{d.license}</span>
            </p>
        </section>
    )
}
