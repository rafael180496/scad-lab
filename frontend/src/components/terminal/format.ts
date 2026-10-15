import {locale} from '../../i18n'

// sessionDate: «15 oct, 13:42» en el idioma activo. Es la hora de pared del
// equipo del usuario (la fecha de modificación de la sesión), no un dato de
// una base remota.
export function sessionDate(ms: number): string {
    if (!ms) return ''
    return new Intl.DateTimeFormat(locale(), {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'}).format(new Date(ms))
}
