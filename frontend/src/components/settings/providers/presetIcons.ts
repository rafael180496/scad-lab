import {Atom, Brain, Cloud, Cpu, Diamond, Gem, Laptop, Network, Rocket, Route, Server, SlidersHorizontal, Sparkles, Terminal, Wind, Zap, type LucideIcon} from 'lucide-react'
import type {PresetKey} from '../../../lib/modelServerPresets'
import type {ServerKind} from '../../../lib/modelServerPresets'
import {cloudPresetForUrl} from '../../../lib/modelServerPresets'

// Íconos genéricos de lucide por atajo: la app no lleva logos de marcas.
export const PRESET_ICON: Record<PresetKey, LucideIcon> = {
    openrouter: Route,
    openai: Sparkles,
    gemini: Gem,
    anthropic: Brain,
    groq: Zap,
    deepseek: Atom,
    mistral: Wind,
    together: Network,
    xai: Rocket,
    ollamaLocal: Laptop,
    ollamaRemote: Network,
    lmstudio: Cpu,
    llamacpp: Terminal,
    vllm: Server,
    openaiOther: SlidersHorizontal,
}

// Ícono de un servidor guardado: el del atajo si es un servicio alojado
// conocido; si no, el del tipo.
export function serverIcon(s: {kind: string; baseUrl: string}): LucideIcon {
    const cloud = cloudPresetForUrl(s.baseUrl)
    if (cloud) return PRESET_ICON[cloud.key]
    if (s.kind === 'anthropic') return PRESET_ICON.anthropic
    return (s.kind as ServerKind) === 'openai' ? Cloud : Diamond
}
