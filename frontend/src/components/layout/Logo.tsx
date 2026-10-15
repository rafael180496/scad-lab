// Marca de ScadLab: un cubo isométrico con la cara superior en el color de
// acento, como una pieza recién salida de la cama de impresión. Mismo dibujo
// que build/appicon.png (ver assets/brand/icon.svg).
export function Logo({className}: {className?: string}) {
    return (
        <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
            <path d="M16 3 28 10 16 17 4 10Z" fill="var(--c-accent)" />
            <path d="M4 10 16 17v12L4 22Z" fill="currentColor" opacity=".55" />
            <path d="M28 10 16 17v12l12-7Z" fill="currentColor" opacity=".85" />
            <path d="M10 13.4 16 17l6-3.6" fill="none" stroke="var(--c-bg)" strokeWidth="1.2" opacity=".55" />
        </svg>
    )
}
