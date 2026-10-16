import {Info} from 'lucide-react'
import {DownloadControls} from './DownloadControls'
import {fmt, useT} from '../../i18n'
import {wasmShortVersion} from '../../lib/engine'

// WasmNotice es el aviso informativo del panel de render cuando el checkpoint se
// renderizó con el motor integrado: no es un error (la pieza salió), pero conviene
// saber por qué es más lento y cómo cambiarlo.
export function WasmNotice({version}: {version: string}) {
    const t = useT()
    return (
        <div className="flex shrink-0 items-start gap-2.5 border-b border-line bg-info/10 px-3 py-2" role="note">
            <Info className="mt-px icon-ui-14 shrink-0 text-info" aria-hidden />
            <div className="flex min-w-0 flex-1 flex-col gap-2">
                <p className="text-ui-12 leading-relaxed text-fg">{fmt(t.render.wasmNotice, {v: wasmShortVersion(version)})}</p>
                <DownloadControls />
            </div>
        </div>
    )
}
