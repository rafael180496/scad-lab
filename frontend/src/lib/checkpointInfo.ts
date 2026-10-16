import type {store} from '../../wailsjs/go/models'
import {fmt, locale, type Dict} from '../i18n'

// Nombres de los CLIs agénticos tal como se muestran (los ids son los de
// ai_providers). Un id desconocido se muestra tal cual.
const AGENT_NAMES: Record<string, string> = {
    claude: 'Claude Code',
    opencode: 'opencode',
    codex: 'Codex',
    antigravity: 'Antigravity',
}

export function agentName(id: string): string {
    return AGENT_NAMES[id] ?? id
}

// Fecha y hora cortas en el idioma activo: la hora de pared del equipo (la fecha
// de modificación del archivo), no un dato de una base remota.
export function checkpointDate(unixSeconds: number): string {
    if (!unixSeconds) return ''
    return new Intl.DateTimeFormat(locale(), {dateStyle: 'medium', timeStyle: 'short'}).format(new Date(unixSeconds * 1000))
}

// El checkpoint tiene un vínculo que se puede retomar: la sesión de un agente de
// la terminal (con su id) o el chat de diseño desde el que se guardó.
export function resumeKind(cp: store.Checkpoint): 'agent' | 'chat' | null {
    if (cp.agentId && cp.sessionId) return 'agent'
    if (cp.chatId) return 'chat'
    return null
}

// checkpointTitle arma el tooltip de una fila: descripción, quién lo creó y
// cuándo. `title` nativo con saltos de línea (lo muestra WebKit y WebView2).
export function checkpointTitle(t: Dict, cp: store.Checkpoint): string {
    const lines = [cp.description || cp.fileName]
    if (cp.label || cp.description) lines.push(fmt(t.checkpoints.tip.file, {file: cp.fileName}))
    if (cp.agentId) {
        const by = fmt(t.checkpoints.tip.by, {agent: agentName(cp.agentId)})
        lines.push(cp.sessionId ? `${by} · ${fmt(t.checkpoints.tip.session, {id: cp.sessionId.slice(0, 8)})}` : by)
    } else if (cp.chatId) {
        lines.push(t.checkpoints.tip.chat)
    }
    const date = checkpointDate(cp.fileMtime)
    if (date) lines.push(date)
    return lines.join('\n')
}
