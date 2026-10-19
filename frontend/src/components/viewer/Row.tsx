// Una fila «rótulo … valor» de las tarjetas de medidas (el panel de la pieza, el diálogo de importar). Va aparte de
// Dimensions porque esa tarjeta arrastra la geometría (three) y el diálogo de importar está en el chunk principal.
export function Row({label, hint, children}: {label: string; hint: string; children: React.ReactNode}) {
    return (
        <div className="flex items-baseline justify-between gap-4" title={hint}>
            <span className="font-mono text-ui-10 tracking-[0.1em] text-faint uppercase">{label}</span>
            <span className="font-mono text-ui-11 text-fg tabular-nums">{children}</span>
        </div>
    )
}
