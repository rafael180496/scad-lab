import {useId, useMemo} from 'react'
import type {main} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

/**
 * La lista de skills de un workspace (S14), con una casilla por skill: nombre, descripción, si es
 * incluida o propia y cuál es la de marca. La de la marca de la impresora elegida va señalada
 * («Tu impresora»); las de otras marcas llevan «Marca» a secas, para que se vea por qué no están
 * marcadas. Se usa en «Nuevo workspace» y en «Configuración del workspace».
 */
export function SkillsChecklist({
    skills,
    value,
    onChange,
    printerSkill,
    brandSkills,
    className,
    compact,
}: {
    skills: main.SkillView[]
    value: string[]
    onChange: (next: string[]) => void
    /** La skill de la marca de la impresora elegida ('' = ninguna). */
    printerSkill: string
    /** Los nombres de todas las skills de marca. */
    brandSkills: ReadonlySet<string>
    className?: string
    /** Descripción de una línea y filas más bajas (el diálogo de «Nuevo workspace», que tiene mucho más que mostrar). */
    compact?: boolean
}) {
    const t = useT()
    const id = useId()
    const on = new Set(value)
    // La de la marca de la impresora primero (es la que justifica este selector), después las comunes y al final las de
    // otras marcas, que casi nunca se quieren: cada grupo en orden alfabético.
    const ordered = useMemo(() => {
        const rank = (s: main.SkillView) => (s.name === printerSkill ? 0 : brandSkills.has(s.name) ? 2 : 1)
        return [...skills].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name))
    }, [skills, printerSkill, brandSkills])
    const toggle = (name: string, checked: boolean) => {
        const next = new Set(on)
        if (checked) next.add(name)
        else next.delete(name)
        // Alfabético: la selección no depende del orden de los clics.
        onChange([...next].sort())
    }
    return (
        <div role="group" aria-label={t.wsConfig.skillsTitle} className={cn('divide-y divide-line overflow-auto rounded-lg border border-line', className)}>
            {ordered.map((s, i) => {
                const own = s.source === 'user'
                const isPrinter = s.name === printerSkill
                const isBrand = brandSkills.has(s.name)
                const cid = `${id}-${i}`
                return (
                    <label
                        key={s.name}
                        htmlFor={cid}
                        title={fmt(t.wsConfig.skillHint, {name: s.name})}
                        className={cn('flex cursor-pointer items-start gap-2.5 px-3 transition-colors hover:bg-surface-2', compact ? 'py-1.5' : 'py-2', isPrinter && 'bg-accent-soft/40')}
                    >
                        <input
                            id={cid}
                            type="checkbox"
                            className="mt-0.5 icon-ui-14 shrink-0 accent-accent"
                            checked={on.has(s.name)}
                            onChange={(e) => toggle(s.name, e.target.checked)}
                        />
                        <span className="min-w-0 flex-1">
                            <span className="flex flex-wrap items-center gap-1.5">
                                <span className="truncate font-mono text-ui-12 font-medium">{s.name}</span>
                                <span className={cn('shrink-0 rounded px-1.5 py-px font-mono text-ui-10', own ? 'bg-accent-soft text-accent' : 'bg-surface-2 text-faint')}>
                                    {own ? t.settings.user : t.settings.builtin}
                                </span>
                                {isPrinter ? (
                                    <span className="shrink-0 rounded bg-accent px-1.5 py-px font-mono text-ui-10 text-accent-fg" title={t.wsConfig.brandSkillOursHint}>
                                        {t.wsConfig.brandSkillOurs}
                                    </span>
                                ) : isBrand ? (
                                    <span className="shrink-0 rounded border border-line px-1.5 py-px font-mono text-ui-10 text-muted" title={t.wsConfig.brandSkillHint}>
                                        {t.wsConfig.brandSkill}
                                    </span>
                                ) : null}
                            </span>
                            <span className={cn('mt-0.5 block text-ui-11-5 leading-snug text-faint', compact ? 'line-clamp-1' : 'line-clamp-2')} title={s.description}>{s.description}</span>
                        </span>
                    </label>
                )
            })}
        </div>
    )
}
