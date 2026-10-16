import {useCallback, useEffect, useState} from 'react'
import {CheckCircle2, CircleDashed, FolderOpen, Plus, RefreshCw, Coffee, Heart} from 'lucide-react'
import type {detect, main, printers} from '../../../wailsjs/go/models'
import {store} from '../../../wailsjs/go/models'
import {
    ChooseDirectory,
    ChooseFile,
    CreateSkill,
    DetectSlicers,
    DetectTools,
    EffectiveWorkspacesRoot,
    ListMaterials,
    ListProviders,
    ListSkills,
    OllamaModels,
    OpenSkillsFolder,
    SaveMaterial,
    SaveProvider,
    SaveSettings,
    SetSkillEnabled,
} from '../../../wailsjs/go/main/App'
import {BrowserOpenURL} from '../../../wailsjs/runtime/runtime'
import {BUY_ME_A_COFFEE_URL, PATREON_JOIN_URL, PATREON_PAGE_URL} from '../../lib/support'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field, Input, Select, Toggle} from '../ui/Field'
import {toast} from '../ui/Toast'
import {fmt, LANGUAGES, normalizeLang, setLanguage, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {CURRENCIES} from '../../lib/printFormat'
import {reloadMaterials} from '../../lib/materials'
import {applyTheme} from '../../lib/theme'
import {cn} from '../../lib/cn'
import type {Appearance} from '../../lib/appearance'
import {AppearanceSettings} from './AppearanceSettings'
import {McpSettings} from './McpSettings'

export type SettingsTab = 'general' | 'openscad' | 'ai' | 'printing' | 'skills' | 'about'
type Tab = SettingsTab
const TABS: Tab[] = ['general', 'openscad', 'ai', 'printing', 'skills', 'about']

type ProviderCfg = {baseUrl?: string; model?: string; apiKeyEnv?: string}

function parseCfg(raw: unknown): ProviderCfg {
    if (raw && typeof raw === 'object') return raw as ProviderCfg
    try {
        return JSON.parse(String(raw ?? '{}'))
    } catch {
        return {}
    }
}

function ToolStatus({tool}: {tool?: detect.Tool}) {
    const t = useT()
    if (!tool) return null
    return tool.found ? (
        <span className="selectable flex min-w-0 items-center gap-1.5 text-ui-11-5 text-ok" title={tool.path}>
            <CheckCircle2 className="icon-ui-14 shrink-0" />
            <span className="truncate">
                {t.settings.found}
                {tool.version ? ` · ${fmt(t.settings.version, {v: tool.version})}` : ''}
            </span>
        </span>
    ) : (
        <span className="flex items-center gap-1.5 text-ui-11-5 text-faint">
            <CircleDashed className="icon-ui-14" />
            {t.settings.notFound}
        </span>
    )
}

export function SettingsDialog({
    open,
    onClose,
    boot,
    printerList,
    onSaved,
    appearance,
    onAppearance,
    onResetAppearance,
    onResetLayout,
    initialTab = 'general',
    workspaceId = null,
    workspaceName,
}: {
    open: boolean
    // initialTab es la pestaña con la que abre: el aviso «OpenSCAD no
    // encontrado» del visor lleva directo a OpenSCAD.
    initialTab?: Tab
    // Workspace abierto: Configuración → IA muestra el estado de sus archivos MCP.
    workspaceId?: number | null
    workspaceName?: string
    onClose: () => void
    boot: main.Bootstrap
    printerList: printers.Printer[]
    onSaved: (s: store.Settings) => void
    // Apariencia y diseño se aplican y guardan al instante, fuera de «Guardar»:
    // el diálogo recibe el valor vigente y avisa de cada cambio.
    appearance: Appearance
    onAppearance: (patch: Partial<Appearance>) => void
    onResetAppearance: () => void
    onResetLayout: () => void
}) {
    const t = useT()
    const [tab, setTab] = useState<Tab>(initialTab)
    const [s, setS] = useState<store.Settings>(boot.settings)
    const [defaultRoot, setDefaultRoot] = useState('')
    const [tools, setTools] = useState<detect.Tool[]>([])
    const [slicers, setSlicers] = useState<main.Slicer[]>([])
    const [detecting, setDetecting] = useState(false)
    const [providers, setProviders] = useState<store.AIProvider[]>([])
    const [models, setModels] = useState<Record<string, string[]>>({})
    const [materials, setMaterials] = useState<store.Material[]>([])
    const [skills, setSkills] = useState<main.SkillView[]>([])
    const [newSkill, setNewSkill] = useState('')

    const detectAll = useCallback(async () => {
        setDetecting(true)
        try {
            setTools(await DetectTools())
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setDetecting(false)
        }
    }, [])

    useEffect(() => {
        if (!open) return
        setS(boot.settings)
        setTab(initialTab)
        EffectiveWorkspacesRoot().then(setDefaultRoot).catch(() => {})
        ListProviders().then(setProviders).catch((e) => toast.error(errorText(e)))
        ListMaterials().then(setMaterials).catch((e) => toast.error(errorText(e)))
        ListSkills().then(setSkills).catch((e) => toast.error(errorText(e)))
        DetectSlicers().then((l) => setSlicers(l ?? [])).catch(() => {})
        void detectAll()
    }, [open, boot.settings, detectAll, initialTab])

    const set = <K extends keyof store.Settings>(k: K, v: store.Settings[K]) => setS((prev) => store.Settings.createFrom({...prev, [k]: v}))

    const save = async () => {
        try {
            // La apariencia vigente se guarda aparte y al instante: el borrador
            // del diálogo trae la de cuando se abrió y no debe pisarla.
            const merged = store.Settings.createFrom({...s, ...appearance})
            await SaveSettings(merged)
            setLanguage(normalizeLang(s.language))
            applyTheme(s.theme)
            onSaved(merged)
            toast.ok(t.settings.saved)
            onClose()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const saveProvider = async (p: store.AIProvider) => {
        setProviders((list) => list.map((x) => (x.id === p.id ? p : x)))
        try {
            await SaveProvider(p)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const tool = (id: string) => tools.find((x) => x.id === id)
    const cliProviders = providers.filter((p) => p.kind === 'cli')
    const serverProviders = providers.filter((p) => p.kind !== 'cli')

    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={t.settings.title}
            closeLabel={t.settings.close}
            className="w-[860px]"
            footer={
                <>
                    {/* Apoyo al proyecto: a la izquierda y discreto, para que no
                        compita con Guardar. Abre el navegador del sistema. */}
                    <div className="mr-auto flex items-center gap-1">
                        <Button variant="ghost" title={t.settings.supportCoffeeHint} onClick={() => BrowserOpenURL(BUY_ME_A_COFFEE_URL)}>
                            <Coffee className="icon-ui-14 text-warn" />
                            {t.settings.supportCoffee}
                        </Button>
                        <Button variant="ghost" title={t.settings.supportPatreonHint} onClick={() => BrowserOpenURL(PATREON_JOIN_URL)}>
                            <Heart className="icon-ui-14 text-accent" />
                            {t.settings.supportPatreon}
                        </Button>
                    </div>
                    <Button title={t.settings.close} onClick={onClose}>
                        {t.settings.close}
                    </Button>
                    <Button variant="primary" title={t.settings.save} onClick={save}>
                        {t.settings.save}
                    </Button>
                </>
            }
        >
            <div className="flex min-h-[460px]">
                {/* La barra de pestañas se queda a la vista aunque la página de la derecha se desplace. */}
                <div className="w-[168px] shrink-0 border-r border-line">
                <nav className="sticky top-0 flex flex-col gap-0.5 p-2" role="tablist">
                    {TABS.map((id) => (
                        <button
                            key={id}
                            type="button"
                            role="tab"
                            aria-selected={tab === id}
                            title={t.settings.tabs[id]}
                            onClick={() => setTab(id)}
                            className={cn(
                                'h-8 rounded-md px-2.5 text-left text-ui-12-5 transition-colors duration-100',
                                tab === id ? 'bg-accent-soft text-fg' : 'text-muted hover:bg-surface-2 hover:text-fg',
                            )}
                        >
                            {t.settings.tabs[id]}
                        </button>
                    ))}
                </nav>
                </div>

                <div className="min-w-0 flex-1 overflow-auto p-5">
                    {tab === 'general' && (
                        <div className="flex flex-col gap-4">
                            <Field label={t.settings.language}>
                                <Select value={s.language} onChange={(e) => set('language', e.target.value)}>
                                    {LANGUAGES.map((l) => (
                                        <option key={l.id} value={l.id}>
                                            {l.label}
                                        </option>
                                    ))}
                                </Select>
                            </Field>
                            <Field label={t.settings.theme}>
                                <Select value={s.theme} onChange={(e) => set('theme', e.target.value)}>
                                    {(['dark', 'light', 'system'] as const).map((k) => (
                                        <option key={k} value={k}>
                                            {t.settings.themes[k]}
                                        </option>
                                    ))}
                                </Select>
                            </Field>
                            <Field label={t.settings.workspacesRoot} hint={fmt(t.settings.workspacesRootHint, {root: defaultRoot})}>
                                <div className="flex gap-2">
                                    <Input value={s.workspacesRoot} onChange={(e) => set('workspacesRoot', e.target.value)} />
                                    <Button
                                        title={t.settings.choose}
                                        onClick={async () => {
                                            const dir = await ChooseDirectory(t.settings.workspacesRoot, s.workspacesRoot || defaultRoot)
                                            if (dir) set('workspacesRoot', dir)
                                        }}
                                    >
                                        {t.settings.choose}
                                    </Button>
                                </div>
                            </Field>

                            <div className="mt-2 flex flex-col gap-4 border-t border-line pt-4">
                                <span className="font-mono text-ui-10-5 tracking-[0.12em] text-faint uppercase">{t.settings.editor}</span>
                                <div className="grid grid-cols-2 gap-4">
                                    <Field label={t.settings.editorIndent} hint={t.settings.editorIndentHint}>
                                        <Select
                                            value={String(s.editorIndent === 2 ? 2 : 4)}
                                            title={t.settings.editorIndent}
                                            onChange={(e) => set('editorIndent', Number(e.target.value))}
                                        >
                                            {[2, 4].map((n) => (
                                                <option key={n} value={n}>
                                                    {fmt(t.settings.indentSpaces, {n})}
                                                </option>
                                            ))}
                                        </Select>
                                    </Field>
                                </div>
                                <div className="flex items-start justify-between gap-4 rounded-lg border border-line px-3 py-2.5">
                                    <div className="flex flex-col gap-0.5">
                                        <span className="text-ui-12-5 font-medium">{t.settings.editorWordWrap}</span>
                                        <span className="text-ui-11-5 leading-relaxed text-faint">{t.settings.editorWordWrapHint}</span>
                                    </div>
                                    <Toggle checked={s.editorWordWrap} title={t.settings.editorWordWrap} onChange={(v) => set('editorWordWrap', v)} />
                                </div>
                                <div className="flex items-start justify-between gap-4 rounded-lg border border-line px-3 py-2.5">
                                    <div className="flex flex-col gap-0.5">
                                        <span className="text-ui-12-5 font-medium">{t.settings.editorMinimap}</span>
                                        <span className="text-ui-11-5 leading-relaxed text-faint">{t.settings.editorMinimapHint}</span>
                                    </div>
                                    <Toggle checked={s.editorMinimap} title={t.settings.editorMinimap} onChange={(v) => set('editorMinimap', v)} />
                                </div>
                            </div>

                            <AppearanceSettings
                                appearance={appearance}
                                onChange={onAppearance}
                                onResetAppearance={onResetAppearance}
                                onResetLayout={onResetLayout}
                            />
                        </div>
                    )}

                    {tab === 'openscad' && (
                        <div className="flex flex-col gap-4">
                            <Field label={t.settings.openscadPath} hint={t.settings.openscadPathHint}>
                                <div className="flex gap-2">
                                    <Input value={s.openscadPath} onChange={(e) => set('openscadPath', e.target.value)} />
                                    <Button
                                        title={t.settings.choose}
                                        onClick={async () => {
                                            const f = await ChooseFile(t.settings.openscadPath)
                                            if (f) set('openscadPath', f)
                                        }}
                                    >
                                        {t.settings.choose}
                                    </Button>
                                </div>
                            </Field>
                            <div className="flex items-center justify-between rounded-lg border border-line bg-surface-2 px-3 py-2.5">
                                <span className="font-mono text-ui-12">openscad</span>
                                <div className="flex items-center gap-3">
                                    <ToolStatus tool={tool('openscad')} />
                                    <Button variant="ghost" title={t.settings.detect} onClick={detectAll} disabled={detecting}>
                                        <RefreshCw className={cn('icon-ui-14', detecting && 'animate-spin')} />
                                        {detecting ? t.settings.detecting : t.settings.detect}
                                    </Button>
                                </div>
                            </div>
                            {tools.length > 0 && !tool('openscad')?.found ? (
                                <p className="text-ui-12 leading-relaxed text-warn">{t.settings.openscadMissing}</p>
                            ) : null}
                            <Field label={t.settings.renderBackend}>
                                <Select value={s.renderBackend} onChange={(e) => set('renderBackend', e.target.value)}>
                                    <option value="manifold">{t.settings.renderBackends.manifold}</option>
                                    <option value="cgal">{t.settings.renderBackends.cgal}</option>
                                </Select>
                            </Field>
                            <div className="flex items-start justify-between gap-4 rounded-lg border border-line px-3 py-2.5">
                                <div className="flex flex-col gap-0.5">
                                    <span className="text-ui-12-5 font-medium">{t.settings.autoRender}</span>
                                    <span className="text-ui-11-5 leading-relaxed text-faint">{t.settings.autoRenderHint}</span>
                                </div>
                                <Toggle checked={s.autoRender} title={t.settings.autoRender} onChange={(v) => set('autoRender', v)} />
                            </div>
                            <Field label={t.settings.renderTimeout} hint={t.settings.renderTimeoutHint}>
                                <Input
                                    type="number"
                                    min={10}
                                    max={3600}
                                    step={10}
                                    className="w-32"
                                    title={t.settings.renderTimeout}
                                    value={s.renderTimeoutSec}
                                    onChange={(e) => set('renderTimeoutSec', Number(e.target.value))}
                                />
                            </Field>
                        </div>
                    )}

                    {tab === 'ai' && (
                        <div className="flex flex-col gap-5">
                            <div className="flex flex-col gap-2">
                                <div className="flex items-end justify-between">
                                    <div>
                                        <h3 className="text-ui-13 font-semibold">{t.settings.agents}</h3>
                                        <p className="text-ui-11-5 text-faint">{t.settings.agentsHint}</p>
                                    </div>
                                    <Button variant="ghost" title={t.settings.detect} onClick={detectAll} disabled={detecting}>
                                        <RefreshCw className={cn('icon-ui-14', detecting && 'animate-spin')} />
                                        {t.settings.detect}
                                    </Button>
                                </div>
                                <div className="divide-y divide-line rounded-lg border border-line">
                                    {cliProviders.map((p) => (
                                        <div key={p.id} className="grid grid-cols-[130px_150px_minmax(0,1fr)_auto] items-center gap-3 px-3 py-2">
                                            <span className="text-ui-12-5 font-medium">{p.name}</span>
                                            <Input
                                                className="h-7 font-mono text-ui-11-5"
                                                value={p.command}
                                                title={t.settings.command}
                                                onChange={(e) => setProviders((l) => l.map((x) => (x.id === p.id ? store.AIProvider.createFrom({...x, command: e.target.value}) : x)))}
                                                onBlur={() => saveProvider(p)}
                                            />
                                            <ToolStatus tool={tool(p.id)} />
                                            <Toggle
                                                checked={p.enabled}
                                                title={t.settings.enabled}
                                                onChange={(v) => saveProvider(store.AIProvider.createFrom({...p, enabled: v}))}
                                            />
                                        </div>
                                    ))}
                                </div>
                                <Field label={t.settings.defaultAgent}>
                                    <Select value={s.defaultAgent} onChange={(e) => set('defaultAgent', e.target.value)}>
                                        {cliProviders.map((p) => (
                                            <option key={p.id} value={p.id}>
                                                {p.name}
                                            </option>
                                        ))}
                                    </Select>
                                </Field>
                            </div>

                            <McpSettings workspaceId={workspaceId} workspaceName={workspaceName} />

                            <div className="flex flex-col gap-2">
                                <h3 className="text-ui-13 font-semibold">{t.settings.modelServers}</h3>
                                {serverProviders.map((p) => {
                                    const cfg = parseCfg(p.config)
                                    const update = (patch: ProviderCfg) =>
                                        setProviders((l) =>
                                            l.map((x) => (x.id === p.id ? store.AIProvider.createFrom({...x, config: {...cfg, ...patch}}) : x)),
                                        )
                                    return (
                                        <div key={p.id} className="flex flex-col gap-3 rounded-lg border border-line p-3">
                                            <div className="flex items-center justify-between">
                                                <span className="text-ui-12-5 font-medium">{p.name}</span>
                                                <Toggle
                                                    checked={p.enabled}
                                                    title={t.settings.enabled}
                                                    onChange={(v) => saveProvider(store.AIProvider.createFrom({...p, enabled: v}))}
                                                />
                                            </div>
                                            <div className="grid grid-cols-2 gap-3" onBlur={() => saveProvider(p)}>
                                                {p.kind !== 'anthropic' ? (
                                                    <Field label={t.settings.baseUrl}>
                                                        <Input value={cfg.baseUrl ?? ''} onChange={(e) => update({baseUrl: e.target.value})} />
                                                    </Field>
                                                ) : null}
                                                <Field label={t.settings.model}>
                                                    {models[p.id]?.length ? (
                                                        <Select value={cfg.model ?? ''} onChange={(e) => update({model: e.target.value})}>
                                                            <option value="">—</option>
                                                            {models[p.id].map((m) => (
                                                                <option key={m} value={m}>
                                                                    {m}
                                                                </option>
                                                            ))}
                                                        </Select>
                                                    ) : (
                                                        <Input value={cfg.model ?? ''} onChange={(e) => update({model: e.target.value})} />
                                                    )}
                                                </Field>
                                                {p.kind !== 'ollama' ? (
                                                    <Field label={t.settings.apiKeyEnv} hint={t.settings.apiKeyEnvHint}>
                                                        <Input className="font-mono" value={cfg.apiKeyEnv ?? ''} onChange={(e) => update({apiKeyEnv: e.target.value})} />
                                                    </Field>
                                                ) : null}
                                            </div>
                                            {p.kind === 'ollama' ? (
                                                <div className="flex items-center gap-3">
                                                    <Button
                                                        title={t.settings.testConnection}
                                                        onClick={async () => {
                                                            try {
                                                                const list = await OllamaModels(cfg.baseUrl ?? '')
                                                                setModels((m) => ({...m, [p.id]: list}))
                                                                toast.ok(fmt(t.settings.modelsFound, {n: list.length}))
                                                            } catch (e) {
                                                                toast.error(errorText(e))
                                                            }
                                                        }}
                                                    >
                                                        {t.settings.testConnection}
                                                    </Button>
                                                    {models[p.id] ? (
                                                        <span className="text-ui-11-5 text-ok">{fmt(t.settings.modelsFound, {n: models[p.id].length})}</span>
                                                    ) : null}
                                                </div>
                                            ) : null}
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    )}

                    {tab === 'printing' && (
                        <div className="flex flex-col gap-4">
                            <Field label={t.settings.defaultPrinter}>
                                <Select value={s.defaultPrinter} onChange={(e) => set('defaultPrinter', e.target.value)}>
                                    {printerList.map((p) => (
                                        <option key={p.id} value={p.id}>
                                            {p.name}
                                        </option>
                                    ))}
                                </Select>
                            </Field>
                            {(() => {
                                const p = printerList.find((x) => x.id === s.defaultPrinter)
                                return p ? (
                                    <p className="-mt-2 font-mono text-ui-11-5 text-faint">
                                        {fmt(t.settings.buildVolume, {x: p.build_volume.x, y: p.build_volume.y, z: p.build_volume.z})} ·{' '}
                                        {fmt(t.settings.colors, {n: p.multicolor_slots || 1})}
                                    </p>
                                ) : null
                            })()}
                            <div className="grid grid-cols-2 gap-3">
                                <Field label={t.settings.material}>
                                    <Select value={s.defaultMaterial} onChange={(e) => set('defaultMaterial', e.target.value)}>
                                        {materials.map((m) => (
                                            <option key={m.id} value={m.id}>
                                                {m.name}
                                            </option>
                                        ))}
                                    </Select>
                                </Field>
                                <Field label={t.settings.infill}>
                                    <Input
                                        type="number"
                                        min={0}
                                        max={100}
                                        value={s.infillPercent}
                                        onChange={(e) => set('infillPercent', Number(e.target.value))}
                                    />
                                </Field>
                            </div>
                            <p className="-mb-1 text-ui-11-5 leading-relaxed text-faint">{t.settings.estimateIntro}</p>
                            <div className="grid grid-cols-4 gap-3">
                                <Field label={t.settings.wallLoops}>
                                    <Input
                                        type="number"
                                        min={1}
                                        max={10}
                                        step={1}
                                        value={s.wallLoops}
                                        title={t.settings.wallLoopsHint}
                                        onChange={(e) => set('wallLoops', Math.round(Number(e.target.value)))}
                                    />
                                </Field>
                                <Field label={t.settings.lineWidth}>
                                    <Input
                                        type="number"
                                        min={0.2}
                                        max={1.5}
                                        step={0.01}
                                        value={s.lineWidthMm}
                                        title={t.settings.lineWidthHint}
                                        onChange={(e) => set('lineWidthMm', Number(e.target.value))}
                                    />
                                </Field>
                                <Field label={t.settings.filamentDiameter}>
                                    <Input
                                        type="number"
                                        min={1}
                                        max={3.5}
                                        step={0.05}
                                        value={s.filamentDiameterMm}
                                        title={t.settings.filamentDiameterHint}
                                        onChange={(e) => set('filamentDiameterMm', Number(e.target.value))}
                                    />
                                </Field>
                                <Field label={t.settings.currency}>
                                    <Select value={s.currency} title={t.settings.currencyHint} onChange={(e) => set('currency', e.target.value)}>
                                        {(CURRENCIES.includes(s.currency) ? CURRENCIES : [s.currency, ...CURRENCIES]).map((c) => (
                                            <option key={c} value={c}>
                                                {c}
                                            </option>
                                        ))}
                                    </Select>
                                </Field>
                            </div>
                            <Field label={t.settings.bambuPath} hint={t.settings.bambuPathHint}>
                                <div className="flex gap-2">
                                    <Input
                                        value={s.bambuStudioPath}
                                        title={t.settings.bambuPath}
                                        onChange={(e) => set('bambuStudioPath', e.target.value)}
                                    />
                                    <Button
                                        title={t.settings.choose}
                                        onClick={async () => {
                                            const f = await ChooseFile(t.settings.bambuPath)
                                            if (f) set('bambuStudioPath', f)
                                        }}
                                    >
                                        {t.settings.choose}
                                    </Button>
                                </div>
                            </Field>
                            {(() => {
                                const b = slicers.find((x) => x.id === 'bambu-studio')
                                return b ? (
                                    <div className="-mt-1 flex items-center justify-between gap-3 rounded-lg border border-line bg-surface-2 px-3 py-2.5">
                                        <span className="font-mono text-ui-12">{t.settings.bambuStudio}</span>
                                        {b.found ? (
                                            <span className="selectable flex min-w-0 items-center gap-1.5 text-ui-11-5 text-ok" title={b.path}>
                                                <CheckCircle2 className="icon-ui-14 shrink-0" />
                                                <span className="truncate">
                                                    {t.settings.found} · {b.path}
                                                </span>
                                            </span>
                                        ) : (
                                            <span className="flex items-center gap-1.5 text-ui-11-5 text-faint">
                                                <CircleDashed className="icon-ui-14" />
                                                {t.settings.notFound}
                                            </span>
                                        )}
                                    </div>
                                ) : null
                            })()}
                            <h3 className="mt-2 text-ui-13 font-semibold">{t.settings.materials}</h3>
                            <div className="divide-y divide-line rounded-lg border border-line">
                                <div className="grid grid-cols-[1fr_140px_140px] gap-3 px-3 py-1.5 font-mono text-ui-10-5 tracking-wider text-faint uppercase">
                                    <span>{t.settings.material}</span>
                                    <span>{t.settings.density}</span>
                                    <span>{t.settings.costPerKg}</span>
                                </div>
                                {materials.map((m) => {
                                    const upd = (patch: Partial<store.Material>) =>
                                        setMaterials((l) => l.map((x) => (x.id === m.id ? store.Material.createFrom({...x, ...patch}) : x)))
                                    // Tras guardar, la estimación del panel «Impresión» se vuelve a pedir
                                    // con la densidad y el costo nuevos.
                                    const persist = () =>
                                        SaveMaterial(materials.find((x) => x.id === m.id) ?? m)
                                            .then(() => reloadMaterials())
                                            .catch((e) => toast.error(errorText(e)))
                                    return (
                                        <div key={m.id} className="grid grid-cols-[1fr_140px_140px] items-center gap-3 px-3 py-1.5">
                                            <span className="text-ui-12-5">{m.name}</span>
                                            <Input
                                                className="h-7"
                                                type="number"
                                                step="0.01"
                                                value={m.densityGCm3}
                                                onChange={(e) => upd({densityGCm3: Number(e.target.value)})}
                                                onBlur={persist}
                                            />
                                            <Input
                                                className="h-7"
                                                type="number"
                                                step="0.5"
                                                value={m.costPerKg}
                                                onChange={(e) => upd({costPerKg: Number(e.target.value)})}
                                                onBlur={persist}
                                            />
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    )}

                    {tab === 'skills' && (
                        <div className="flex flex-col gap-4">
                            <p className="text-ui-12 leading-relaxed text-muted">{t.settings.skillsIntro}</p>
                            <div className="divide-y divide-line rounded-lg border border-line">
                                {skills.map((k) => (
                                    <div key={k.name} className="flex items-start gap-3 px-3 py-2.5">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex items-center gap-2">
                                                <span className="font-mono text-ui-12 font-medium">{k.name}</span>
                                                <span
                                                    className={cn(
                                                        'rounded px-1.5 py-px font-mono text-ui-10',
                                                        k.source === 'user' ? 'bg-accent-soft text-accent' : 'bg-surface-2 text-faint',
                                                    )}
                                                >
                                                    {k.source === 'user' ? t.settings.user : t.settings.builtin}
                                                </span>
                                            </div>
                                            <p className="mt-0.5 line-clamp-2 text-ui-11-5 leading-snug text-faint">{k.description}</p>
                                        </div>
                                        <Toggle
                                            checked={k.enabled}
                                            title={t.settings.enabled}
                                            onChange={async (v) => {
                                                setSkills((l) => l.map((x) => (x.name === k.name ? {...x, enabled: v} : x)) as main.SkillView[])
                                                try {
                                                    await SetSkillEnabled(k.name, v)
                                                } catch (e) {
                                                    toast.error(errorText(e))
                                                }
                                            }}
                                        />
                                    </div>
                                ))}
                            </div>
                            <Field label={t.settings.newSkill} hint={t.settings.newSkillHint}>
                                <div className="flex gap-2">
                                    <Input
                                        className="font-mono"
                                        value={newSkill}
                                        placeholder={t.settings.newSkillPlaceholder}
                                        onChange={(e) => setNewSkill(e.target.value.toLowerCase())}
                                    />
                                    <Button
                                        variant="primary"
                                        title={t.settings.newSkillHint}
                                        disabled={!newSkill.trim()}
                                        onClick={async () => {
                                            try {
                                                const dir = await CreateSkill(newSkill.trim())
                                                toast.ok(fmt(t.settings.skillCreated, {dir}))
                                                setNewSkill('')
                                                setSkills(await ListSkills())
                                            } catch (e) {
                                                toast.error(errorText(e))
                                            }
                                        }}
                                    >
                                        <Plus className="icon-ui-14" />
                                        {t.settings.newSkill}
                                    </Button>
                                    <Button title={t.settings.openSkillsFolder} onClick={() => OpenSkillsFolder().catch((e) => toast.error(errorText(e)))}>
                                        <FolderOpen className="icon-ui-14" />
                                    </Button>
                                </div>
                            </Field>
                        </div>
                    )}

                    {tab === 'about' && (
                        <dl className="selectable grid grid-cols-[160px_1fr] gap-x-4 gap-y-2 text-ui-12-5">
                            <dt className="text-muted">{t.settings.aboutVersion}</dt>
                            <dd className="font-mono">{boot.version}</dd>
                            <dt className="text-muted">{t.settings.aboutPlatform}</dt>
                            <dd className="font-mono">{boot.platform}</dd>
                            <dt className="text-muted">{t.settings.aboutData}</dt>
                            <dd className="font-mono break-all">{boot.dataDir}</dd>
                            <dt className="text-muted">{t.settings.aboutSchema}</dt>
                            <dd className="font-mono">{boot.schemaVersion}</dd>
                            <dt className="text-muted">{t.settings.aboutLicense}</dt>
                            <dd>{t.settings.aboutLicenseValue}</dd>
                        </dl>
                    )}
                    {tab === 'about' && (
                        <section className="mt-6 rounded-lg border border-line bg-surface-2 p-4">
                            <h3 className="text-ui-13 font-semibold">{t.settings.supportTitle}</h3>
                            <p className="mt-1 text-ui-12 leading-relaxed text-muted">{t.settings.supportBody}</p>
                            <div className="mt-3 flex flex-wrap gap-2">
                                <Button variant="primary" title={t.settings.supportCoffeeHint} onClick={() => BrowserOpenURL(BUY_ME_A_COFFEE_URL)}>
                                    <Coffee className="icon-ui-14" />
                                    {t.settings.supportCoffee}
                                </Button>
                                <Button title={t.settings.supportPatreonHint} onClick={() => BrowserOpenURL(PATREON_JOIN_URL)}>
                                    <Heart className="icon-ui-14 text-accent" />
                                    {t.settings.supportPatreon}
                                </Button>
                                <Button variant="ghost" title={t.settings.supportPatreonPageHint} onClick={() => BrowserOpenURL(PATREON_PAGE_URL)}>
                                    {t.settings.supportPatreonPage}
                                </Button>
                            </div>
                        </section>
                    )}
                </div>
            </div>
        </Dialog>
    )
}
