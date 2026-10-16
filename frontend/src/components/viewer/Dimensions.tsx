import {useEffect, useState} from 'react'
import type {BufferGeometry} from 'three'
import {ChevronDown, ChevronUp, CircleCheck, TriangleAlert} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {num} from '../../lib/printFormat'
import {getMeshInfo, infoFromGeometry, type MeshFacts} from '../../lib/meshInfo'

export function Row({label, hint, children}: {label: string; hint: string; children: React.ReactNode}) {
    return (
        <div className="flex items-baseline justify-between gap-4" title={hint}>
            <span className="font-mono text-ui-10 tracking-[0.1em] text-faint uppercase">{label}</span>
            <span className="font-mono text-ui-11 text-fg tabular-nums">{children}</span>
        </div>
    )
}

// Dimensions: medidas de la pieza en una esquina del visor. Vienen de Go
// (GetMeshInfo); mientras tanto, o si falla, se muestra lo que sabe la
// geometría. Plegado queda solo el tamaño, en una línea: en la vista dividida
// el panel entero tapaba la pieza.
export function Dimensions({
    meshPath,
    geometry,
    collapsed,
    onCollapsed,
}: {
    meshPath: string
    geometry: BufferGeometry
    collapsed: boolean
    onCollapsed: (collapsed: boolean) => void
}) {
    const t = useT()
    const [facts, setFacts] = useState<MeshFacts>(() => ({info: infoFromGeometry(geometry), exact: false}))

    useEffect(() => {
        let alive = true
        setFacts({info: infoFromGeometry(geometry), exact: false})
        getMeshInfo(meshPath)
            .then((info) => alive && setFacts({info, exact: true}))
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [meshPath, geometry])

    const {info, exact} = facts
    const size = info.size.map((v) => num(v, 1)).join(' × ')
    const toggle = (
        <button
            type="button"
            title={collapsed ? t.viewer.dimsExpand : t.viewer.dimsCollapse}
            aria-label={collapsed ? t.viewer.dimsExpand : t.viewer.dimsCollapse}
            aria-expanded={!collapsed}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={() => onCollapsed(!collapsed)}
            className="-mr-1 inline-flex icon-ui-20 items-center justify-center rounded text-faint transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
        >
            {collapsed ? <ChevronUp className="icon-ui-12" /> : <ChevronDown className="icon-ui-12" />}
        </button>
    )
    if (collapsed) {
        return (
            <div className="pointer-events-auto flex items-center gap-2 rounded-lg border border-line bg-surface/90 py-1 pr-2 pl-3 backdrop-blur-sm">
                <span className="font-mono text-ui-10 tracking-[0.12em] text-muted uppercase">{t.viewer.dims}</span>
                <span className="font-mono text-ui-11 text-fg tabular-nums" title={t.viewer.sizeHint}>
                    {fmt(t.viewer.mm, {v: size})}
                </span>
                {toggle}
            </div>
        )
    }
    return (
        <div className="pointer-events-auto flex min-w-[190px] flex-col gap-1 rounded-lg border border-line bg-surface/90 px-3 py-2 backdrop-blur-sm">
            <div className="flex items-center justify-between">
                <span className="font-mono text-ui-10 tracking-[0.12em] text-muted uppercase" title={exact ? t.viewer.dims : t.viewer.approx}>
                    {t.viewer.dims}
                </span>
                {toggle}
            </div>
            <Row label={t.viewer.size} hint={t.viewer.sizeHint}>
                {fmt(t.viewer.mm, {v: size})}
            </Row>
            <Row label={t.viewer.volume} hint={exact ? t.viewer.volumeHint : t.viewer.approx}>
                {exact ? fmt(t.viewer.cm3, {v: num(info.volumeMm3 / 1000, 2)}) : '—'}
            </Row>
            <Row label={t.viewer.triangles} hint={t.viewer.trianglesHint}>
                {num(info.triangles, 0)}
            </Row>
            {exact ? (
                info.watertight ? (
                    <span className="flex items-center gap-1 text-ui-11 text-ok" title={t.viewer.closedHint}>
                        <CircleCheck className="icon-ui-12" />
                        {t.viewer.closed}
                    </span>
                ) : (
                    <span className="flex items-center gap-1 text-ui-11 text-warn" title={t.viewer.openHint}>
                        <TriangleAlert className="icon-ui-12" />
                        {t.viewer.open}
                    </span>
                )
            ) : null}
        </div>
    )
}
