import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {passwordStrength, type StrengthLevel} from '../../lib/passwordStrength'

const KEYS = ['empty', 'veryWeak', 'weak', 'good', 'strong'] as const
const BAR: Record<StrengthLevel, string> = {0: 'bg-line', 1: 'bg-danger', 2: 'bg-warn', 3: 'bg-info', 4: 'bg-ok'}
const TEXT: Record<StrengthLevel, string> = {0: 'text-faint', 1: 'text-danger', 2: 'text-warn', 3: 'text-info', 4: 'text-ok'}

// Indicador de fuerza: cuatro barras que se llenan y la palabra al lado. Es una
// guía, no una barrera (el piso real son 8 caracteres, lo exige el backend).
export function PasswordStrength({password}: {password: string}) {
    const v = useT().vault
    const level = passwordStrength(password)
    const label = v.strength[KEYS[level]]
    return (
        <div className="flex items-center gap-2" role="status" title={fmt(v.strengthLabel, {level: label})} data-testid="strength" data-level={level}>
            <div className="flex flex-1 gap-1" aria-hidden>
                {[1, 2, 3, 4].map((i) => (
                    <span key={i} className={cn('h-1 flex-1 rounded-full transition-colors duration-150', i <= level ? BAR[level] : 'bg-line')} />
                ))}
            </div>
            <span className={cn('w-20 shrink-0 text-right text-ui-11', TEXT[level])}>{label}</span>
        </div>
    )
}
