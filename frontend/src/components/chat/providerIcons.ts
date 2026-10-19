import {CodeXml, Rocket, Server, Sparkles, SquareTerminal, type LucideIcon} from 'lucide-react'
import {cliIdOf, type ChatProviderItem} from '../../lib/chatProviders'
import {serverIcon} from '../settings/providers/presetIcons'

// Íconos de los proveedores del chat. Los de un CLI son los de mini-tools
// (agentIcon.ts) en lucide; la app no lleva logos de marcas. Un servidor usa el
// de su atajo (OpenRouter, Gemini…) si es un servicio conocido, y si no el de
// su tipo, igual que Configuración → Proveedores de IA.
const CLI_ICONS: Record<string, LucideIcon> = {
    claude: Sparkles,
    codex: SquareTerminal,
    antigravity: Rocket,
    opencode: CodeXml,
}

export function providerIcon(p: Pick<ChatProviderItem, 'id' | 'kind' | 'baseUrl'> | undefined): LucideIcon {
    if (!p) return Server
    if (p.kind === 'cli') return CLI_ICONS[cliIdOf(p.id)] ?? Rocket
    return serverIcon({kind: p.kind, baseUrl: p.baseUrl ?? ''})
}
