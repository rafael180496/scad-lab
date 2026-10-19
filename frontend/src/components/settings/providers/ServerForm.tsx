import {useState} from 'react'
import {ExternalLink, KeyRound, Lightbulb, Plug, Info, CircleAlert, Globe, Cloud} from 'lucide-react'
import {SaveModelServer, TestModelServer} from '../../../../wailsjs/go/main/App'
import {store, type main} from '../../../../wailsjs/go/models'
import {BrowserOpenURL} from '../../../../wailsjs/runtime/runtime'
import {fmt, useT} from '../../../i18n'
import {errorText} from '../../../lib/errors'
import {
    CLOUD_PRESETS,
    LOCAL_PRESETS,
    cloudPresetForUrl,
    composeUrl,
    hostPortOf,
    hostProblem,
    parseHostPortUrl,
    presetByKey,
    validPort,
    type Preset,
    type PresetKey,
    type ServerKind,
} from '../../../lib/modelServerPresets'
import {isLoopbackUrl, looksUnreachable, serverKind} from '../../../lib/modelServers'
import {withVault} from '../../../lib/vault'
import {cn} from '../../../lib/cn'
import {Button} from '../../ui/Button'
import {Field, Input, Select} from '../../ui/Field'
import {DefaultModelField} from './DefaultModelField'
import {ProbeResult, type Probe} from './ProbeResult'
import {PRESET_ICON} from './presetIcons'

// Formulario de alta y edición de un servidor de modelos. Portado de mini-tools
// (ModelServerForm.tsx) sin cabeceras propias ni «aceptar certificado
// autofirmado», y con la clave dentro del llavero del sistema en vez del vault.
//
// **La clave nunca vuelve.** El backend devuelve `hasToken` y nada más. Al
// editar, un campo vacío significa «conservar la guardada» y borrarla es un
// gesto aparte («Quitar clave»): un campo vacío que borrara el secreto lo
// perdería quien solo quería cambiar el nombre.
//
// **Guardar no depende de que la prueba salga bien.** Un servidor puede estar
// apagado ahora; obligar a probar primero sería impedir registrar justo el caso
// que más ayuda necesita. Si la prueba falla, el botón pasa a decir «Guardar
// igual» para que sea una decisión visible.
//
// **Atajos (solo al crear).** Tarjetas arriba que completan nombre, tipo y
// dirección (lib/modelServerPresets.ts). Los que viven en otra máquina piden IP
// y puerto y arman `http://<ip>:<puerto>`; la dirección sigue a la vista y se
// sincroniza en los dos sentidos mientras tenga esa forma — si se la edita a
// otra cosa, el atajo se suelta. El nombre se autocompleta mientras nadie lo
// escribió. No se recuerda nada entre aperturas.

// La ventana que ScadLab le pide a Ollama si el servidor no trae una propia
// (backend: chatDefaultNumCtx).
const DEFAULT_OLLAMA_CONTEXT = 16384
const CONTEXT_CHOICES = [4096, 8192, 16384, 32768, 65536]

interface FormState {
    id: string
    name: string
    kind: ServerKind
    baseUrl: string
    token: string
    clearToken: boolean
    hasToken: boolean
    apiKeyEnv: string
    // Ventana de contexto pedida a Ollama, como texto (el campo admite vacío).
    contextTokens: string
    defaultModel: string
    preset: string
    // Se conservan al editar: guardar no los cambia (sortOrder lo ignora el
    // backend) pero viajan completos para que el objeto sea el que se leyó.
    enabled: boolean
    sortOrder: number
    updatedAt: number
}

function emptyForm(): FormState {
    return {
        id: '',
        name: '',
        kind: 'ollama',
        baseUrl: '',
        token: '',
        clearToken: false,
        hasToken: false,
        apiKeyEnv: '',
        contextTokens: '',
        defaultModel: '',
        preset: '',
        enabled: true,
        sortOrder: 0,
        updatedAt: 0,
    }
}

function fromServer(s: store.ModelServer): FormState {
    return {
        id: s.id,
        name: s.name,
        kind: serverKind(s.kind),
        baseUrl: s.baseUrl,
        token: '',
        clearToken: false,
        hasToken: s.hasToken,
        apiKeyEnv: s.apiKeyEnv,
        contextTokens: s.contextTokens > 0 ? String(s.contextTokens) : '',
        defaultModel: s.defaultModel,
        preset: s.preset,
        enabled: s.enabled,
        sortOrder: s.sortOrder,
        updatedAt: s.updatedAt,
    }
}

const LOCAL_OLLAMA_URL = 'http://localhost:11434'
const placeholderFor = (k: ServerKind) => (k === 'openai' ? 'http://localhost:1234/v1' : k === 'anthropic' ? 'https://api.anthropic.com' : LOCAL_OLLAMA_URL)

export function ServerForm({
    server,
    vault,
    onSaved,
    onCancel,
}: {
    /** null = servidor nuevo. */
    server: store.ModelServer | null
    /** Estado del llavero propio: dice si al guardar se pedirá crear la contraseña. */
    vault: main.VaultStatus | null
    onSaved: (saved: store.ModelServer) => void
    onCancel: () => void
}) {
    const t = useT()
    const p = t.providers
    const [form, setForm] = useState<FormState>(() => (server ? fromServer(server) : emptyForm()))
    const [probe, setProbe] = useState<Probe | null>(null)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    // Atajos: solo al crear. `preset` null = nada elegido o dirección personalizada.
    const isNew = server === null
    const [preset, setPreset] = useState<PresetKey | null>(null)
    const [host, setHost] = useState('')
    const [port, setPort] = useState('11434')
    // El nombre se autocompleta mientras nadie lo escribió a mano.
    const [nameTouched, setNameTouched] = useState(false)
    const activePreset = preset ? presetByKey(preset) : null

    // Nombre sugerido de un atajo; '' si no hay uno que valga todavía.
    function presetName(key: PresetKey, hostValue: string): string {
        const pr = presetByKey(key)
        const h = hostValue.trim()
        if (pr.group === 'cloud') return pr.brand
        if (key === 'openaiOther') return ''
        if (key === 'ollamaLocal') return pr.brand
        if (pr.hostPort) return h !== '' && !hostProblem(h) ? fmt(p.nameWithHost, {brand: pr.brand, host: h}) : key === 'vllm' ? pr.brand : ''
        return pr.brand
    }

    function applyPreset(key: PresetKey) {
        const pr = presetByKey(key)
        const nextPort = pr.hostPort ? String(pr.defaultPort) : port
        if (pr.hostPort) setPort(nextPort)
        setPreset(key)
        setProbe(null)
        setForm((f) => ({
            ...f,
            kind: pr.kind,
            preset: key,
            // Los atajos de Ollama piden la ventana de ScadLab; los compatibles
            // con OpenAI no llevan el campo (el servidor la fija al cargar el modelo).
            contextTokens: '',
            baseUrl: pr.hostPort ? composeUrl(pr, host, nextPort) : pr.fixedUrl,
            name: nameTouched ? f.name : presetName(key, host),
            // Una clave es de UN servicio: la de OpenRouter no se arrastra al pasar
            // a OpenAI. Y el modelo recomendado es el del servicio elegido.
            token: pr.group === 'cloud' || f.kind === 'anthropic' || cloudPresetForUrl(f.baseUrl) ? '' : f.token,
            defaultModel: pr.defaultModel ?? '',
        }))
    }

    function changeHostPort(nextHost: string, nextPort: string) {
        if (!activePreset || !preset) return
        setHost(nextHost)
        setPort(nextPort)
        setProbe(null)
        setForm((f) => ({
            ...f,
            baseUrl: composeUrl(activePreset, nextHost, nextPort),
            name: nameTouched ? f.name : presetName(preset, nextHost),
        }))
    }

    function changeUrl(value: string) {
        setProbe(null)
        // Con un atajo de IP y puerto, la dirección sigue sincronizada mientras
        // conserve esa forma; con cualquier otra, el atajo se suelta.
        const parsed = activePreset?.hostPort ? parseHostPortUrl(activePreset, value) : null
        if (parsed) {
            setHost(parsed.host)
            setPort(parsed.port)
            setForm((f) => ({...f, baseUrl: value, name: nameTouched || !preset ? f.name : presetName(preset, parsed.host)}))
            return
        }
        if (preset) setPreset(null)
        setForm((f) => ({...f, baseUrl: value}))
    }

    const contextValue = Math.min(Number(form.contextTokens.replace(/\D/g, '')) || 0, 4194304)

    // Un servicio alojado no se usa sin clave: sin ella cada turno falla con un
    // 401. Al editar, la ya guardada cuenta (vacío = conservarla), y una variable
    // de entorno también.
    const cloudInfo: Preset | null =
        activePreset?.group === 'cloud' ? activePreset : (cloudPresetForUrl(form.baseUrl) ?? (form.kind === 'anthropic' ? presetByKey('anthropic') : null))
    const isCloud = cloudInfo !== null
    const keyReady = form.token.trim() !== '' || (form.hasToken && !form.clearToken) || form.apiKeyEnv.trim() !== ''
    const keyMissing = isCloud && !keyReady
    const formReady = form.name.trim() !== '' && form.baseUrl.trim() !== '' && !keyMissing

    const hostBad = activePreset?.hostPort === true && hostProblem(host)
    const portBad = activePreset?.hostPort === true && port !== '' && !validPort(port)

    const probeIds = probe?.state === 'done' && probe.probe.ok ? (probe.probe.modelIds ?? []) : []
    const probeError = probe?.state === 'error' ? probe.error : probe?.state === 'done' && !probe.probe.ok ? probe.probe.message : null
    const probeFailed = probeError !== null
    const showRemoteHint = probeFailed && form.kind === 'ollama' && !isLoopbackUrl(form.baseUrl) && looksUnreachable(probeError)

    // El objeto que viaja a Go; el id queda vacío al probar sin la clave guardada.
    const draft = (id: string) =>
        store.ModelServer.createFrom({
            id,
            kind: form.kind,
            preset: form.preset,
            name: form.name.trim(),
            baseUrl: form.baseUrl.trim(),
            apiKeyEnv: form.apiKeyEnv.trim(),
            hasToken: form.hasToken,
            defaultModel: form.defaultModel.trim(),
            // Solo Ollama la admite; el backend igual la descarta en otro tipo.
            contextTokens: form.kind === 'ollama' ? contextValue : 0,
            enabled: form.enabled,
            sortOrder: form.sortOrder,
            updatedAt: form.updatedAt,
        })

    async function save() {
        setSaving(true)
        setError('')
        try {
            onSaved(await withVault(() => SaveModelServer(draft(form.id), form.token.trim(), form.clearToken)))
        } catch (e) {
            setError(errorText(e))
        } finally {
            setSaving(false)
        }
    }

    async function testForm() {
        setProbe({state: 'testing'})
        try {
            // Quitar la clave guardada se prueba como un servidor sin id: el
            // backend no le agrega la guardada.
            const result = await withVault(() => TestModelServer(draft(form.clearToken ? '' : form.id), form.token.trim()))
            setProbe({state: 'done', probe: result})
        } catch (e) {
            setProbe({state: 'error', error: errorText(e)})
        }
    }

    const keyDisabled = form.clearToken

    return (
        <div className="flex flex-col gap-3 rounded-lg border border-accent/50 bg-surface-2/40 p-3" data-testid="server-form">
            <p className="flex items-center gap-2 text-ui-13 font-medium text-fg">
                <Plug className="icon-ui-16 text-accent" />
                {form.id ? fmt(p.formEdit, {name: form.name || '…'}) : p.formNew}
            </p>

            {isNew ? (
                <div data-testid="presets" className="flex flex-col gap-1.5">
                    <div>
                        <p className="text-ui-12 font-medium text-fg">{p.presetTitle}</p>
                        <p className="text-ui-11 text-faint">{p.presetIntro}</p>
                    </div>
                    {(
                        [
                            ['cloud', CLOUD_PRESETS, p.presetGroupCloud, p.presetGroupCloudHint],
                            ['local', LOCAL_PRESETS, p.presetGroupLocal, p.presetGroupLocalHint],
                        ] as const
                    ).map(([group, list, label, hint]) => (
                        <div key={group} role="radiogroup" aria-label={`${p.presetGroupLabel}: ${label}`} className="flex flex-col gap-1">
                            <p className="mt-1 flex items-baseline gap-2 font-mono text-ui-10 tracking-wider text-muted uppercase">
                                {label}
                                <span className="font-sans tracking-normal normal-case text-faint">{hint}</span>
                            </p>
                            <div className="grid grid-cols-3 gap-1.5">
                                {list.map((pr) => {
                                    const selected = preset === pr.key
                                    const Icon = PRESET_ICON[pr.key]
                                    const txt =
                                        pr.group === 'cloud'
                                            ? {name: pr.brand, hint: pr.host ?? '', title: fmt(p.cloudTitle, {brand: pr.brand, host: pr.host ?? ''})}
                                            : p.presets[pr.key as keyof typeof p.presets]
                                    return (
                                        <button
                                            key={pr.key}
                                            type="button"
                                            role="radio"
                                            aria-checked={selected}
                                            data-preset={pr.key}
                                            onClick={() => applyPreset(pr.key)}
                                            title={'title' in txt ? txt.title : ''}
                                            className={cn(
                                                'no-drag flex min-w-0 items-center gap-2 rounded-md border px-2 py-1.5 text-left transition-colors',
                                                selected ? 'border-accent bg-accent-soft' : 'border-line bg-surface hover:border-line-strong hover:bg-surface-2',
                                            )}
                                        >
                                            <Icon className={cn('icon-ui-16 shrink-0', selected ? 'text-accent' : 'text-muted')} />
                                            <span className="min-w-0">
                                                <span className="block truncate text-ui-12 font-medium text-fg">{'name' in txt ? txt.name : pr.brand}</span>
                                                <span className="block truncate font-mono text-ui-10 text-faint">{txt.hint}</span>
                                            </span>
                                        </button>
                                    )
                                })}
                            </div>
                        </div>
                    ))}

                    {activePreset?.hostPort ? (
                        <div className="grid grid-cols-[1fr_7rem] gap-3">
                            <Field label={p.hostLabel}>
                                <Input
                                    value={host}
                                    onChange={(e) => changeHostPort(e.target.value, port)}
                                    placeholder={p.hostPlaceholder}
                                    title={p.hostTitle}
                                    aria-invalid={hostBad}
                                    className={cn('font-mono', hostBad && 'border-danger')}
                                />
                            </Field>
                            <Field label={p.portLabel}>
                                <Input
                                    value={port}
                                    onChange={(e) => changeHostPort(host, e.target.value.replace(/\D/g, '').slice(0, 5))}
                                    inputMode="numeric"
                                    placeholder={String(activePreset.defaultPort)}
                                    title={p.portTitle}
                                    aria-invalid={portBad}
                                    className={cn('font-mono', portBad && 'border-danger')}
                                />
                            </Field>
                            {hostBad || portBad ? (
                                <p className="col-span-2 flex items-center gap-1 text-ui-11 text-danger">
                                    <CircleAlert className="icon-ui-12 shrink-0" />
                                    {hostBad ? p.hostError : p.portError}
                                </p>
                            ) : null}
                        </div>
                    ) : null}
                </div>
            ) : null}

            <div className="grid grid-cols-[1fr_11rem] gap-3">
                <Field label={p.name}>
                    <Input
                        value={form.name}
                        onChange={(e) => {
                            setNameTouched(e.target.value !== '')
                            setForm({...form, name: e.target.value})
                        }}
                        placeholder={p.namePlaceholder}
                        title={p.nameTitle}
                    />
                </Field>
                <Field label={p.kind}>
                    <Select
                        value={form.kind}
                        title={p.kindTitle}
                        onChange={(e) => {
                            setForm({...form, kind: serverKind(e.target.value), preset: ''})
                            if (preset) setPreset(null)
                            setProbe(null)
                        }}
                    >
                        <option value="ollama">{p.kinds.ollama}</option>
                        <option value="openai">{p.kinds.openai}</option>
                        <option value="anthropic">{p.kinds.anthropic}</option>
                    </Select>
                </Field>
            </div>

            <Field label={p.baseUrl} hint={activePreset?.hostPort ? p.urlDerived : undefined}>
                <Input
                    value={form.baseUrl}
                    onChange={(e) => changeUrl(e.target.value)}
                    placeholder={placeholderFor(form.kind)}
                    title={p.baseUrlTitle}
                    className="font-mono"
                />
            </Field>

            {form.baseUrl.trim() !== '' && !isLoopbackUrl(form.baseUrl) ? (
                <p className="flex items-start gap-1.5 rounded-md bg-warn/15 px-2 py-1.5 text-ui-11-5 text-muted">
                    {isCloud ? <Cloud className="icon-ui-14 mt-px shrink-0 text-warn" /> : <Globe className="icon-ui-14 mt-px shrink-0 text-warn" />}
                    {isCloud ? fmt(p.cloudWarning, {host: hostPortOf(form.baseUrl).host}) : p.remoteWarning}
                </p>
            ) : null}

            <div className="flex flex-col gap-1">
                <span className="flex items-center gap-2 font-mono text-ui-10-5 tracking-wider text-muted uppercase">
                    <KeyRound className="icon-ui-12" />
                    <span title={p.apiKeyTitle}>
                        {p.apiKey}{' '}
                        <span className={cn('font-sans normal-case tracking-normal', isCloud ? 'text-danger' : 'text-faint')}>{isCloud ? p.required : p.optional}</span>
                    </span>
                    {isCloud && cloudInfo?.keyUrl ? (
                        <button
                            type="button"
                            onClick={() => BrowserOpenURL(cloudInfo.keyUrl ?? '')}
                            title={fmt(p.getKeyHint, {brand: cloudInfo.brand, url: cloudInfo.keyUrl})}
                            className="no-drag flex items-center gap-1 rounded px-1.5 py-0.5 font-sans text-ui-11-5 normal-case tracking-normal text-accent hover:bg-accent-soft"
                        >
                            <ExternalLink className="icon-ui-12" />
                            {p.getKey}
                        </button>
                    ) : null}
                    {form.hasToken ? (
                        <button
                            type="button"
                            onClick={() => setForm({...form, clearToken: !form.clearToken, token: ''})}
                            title={form.clearToken ? p.apiKeyKeepActionHint : p.apiKeyRemoveHint}
                            className={cn(
                                'no-drag ml-auto rounded px-2 py-0.5 font-sans text-ui-11-5 normal-case tracking-normal',
                                form.clearToken ? 'text-accent hover:bg-accent-soft' : 'text-danger hover:bg-surface-2',
                            )}
                        >
                            {form.clearToken ? p.apiKeyKeepAction : p.apiKeyRemove}
                        </button>
                    ) : null}
                </span>
                <Input
                    type="password"
                    value={form.token}
                    onChange={(e) => {
                        setForm({...form, token: e.target.value})
                        setProbe(null)
                    }}
                    disabled={keyDisabled}
                    placeholder={form.clearToken ? p.apiKeyCleared : form.hasToken ? p.apiKeyKeep : p.apiKeyPlaceholder}
                    title={p.apiKeyTitle}
                    aria-label={p.apiKey}
                    aria-required={isCloud}
                    aria-invalid={keyMissing && form.token === ''}
                    autoComplete="off"
                    className={cn('font-mono', keyMissing && 'border-danger/60')}
                />
                <p className="text-ui-11 text-faint">{p.apiKeyStored}</p>
                {vault && !vault.initialized && form.token.trim() !== '' ? (
                    <p className="flex items-center gap-1.5 text-ui-11 text-info" data-testid="vault-first-time">
                        <KeyRound className="icon-ui-12 shrink-0" />
                        {p.vaultFirstTime}
                    </p>
                ) : vault?.initialized && !vault.unlocked && !vault.remembered && form.token.trim() !== '' ? (
                    <p className="flex items-center gap-1.5 text-ui-11 text-info">
                        <KeyRound className="icon-ui-12 shrink-0" />
                        {p.vaultLocked}
                    </p>
                ) : null}
            </div>

            <Field label={p.env} hint={p.envHint}>
                <Input
                    value={form.apiKeyEnv}
                    onChange={(e) => {
                        setForm({...form, apiKeyEnv: e.target.value})
                        setProbe(null)
                    }}
                    placeholder={p.envPlaceholder}
                    title={p.envTitle}
                    className="font-mono"
                />
            </Field>

            <div className="flex flex-col gap-1">
                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase" title={p.defaultModelTitle}>
                    {p.defaultModel} <span className="font-sans normal-case tracking-normal text-faint">{p.optional}</span>
                </span>
                <DefaultModelField
                    value={form.defaultModel}
                    onChange={(v) => setForm({...form, defaultModel: v})}
                    modelIds={probeIds}
                    placeholder={cloudInfo?.defaultModel ?? p.defaultModelPlaceholder}
                />
                <p className="text-ui-11 text-faint">{probeIds.length > 0 ? p.defaultModelFromProbe : p.defaultModelNeedsTest}</p>
            </div>

            {form.kind === 'ollama' ? (
                <div className="flex flex-col gap-1.5 rounded-md border border-line bg-surface px-2.5 py-2">
                    <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase" title={p.contextTitle}>
                        {p.context} <span className="font-sans normal-case tracking-normal text-faint">{p.optional}</span>
                    </span>
                    <p className="text-ui-11 text-faint">{fmt(p.contextHint, {n: DEFAULT_OLLAMA_CONTEXT.toLocaleString()})}</p>
                    <div className="flex flex-wrap items-center gap-1.5">
                        <Input
                            value={form.contextTokens}
                            onChange={(e) => {
                                setForm({...form, contextTokens: e.target.value.replace(/\D/g, '').slice(0, 7)})
                                setProbe(null)
                            }}
                            inputMode="numeric"
                            placeholder={String(DEFAULT_OLLAMA_CONTEXT)}
                            title={p.contextTitle}
                            aria-label={p.context}
                            className="h-7 w-28 font-mono"
                        />
                        <span className="text-ui-11 text-faint">{p.contextUnit}</span>
                        {CONTEXT_CHOICES.map((n) => (
                            <button
                                key={n}
                                type="button"
                                onClick={() => setForm({...form, contextTokens: String(n)})}
                                title={fmt(p.contextChoice, {n: n.toLocaleString()})}
                                className={cn(
                                    'no-drag rounded border px-1.5 py-0.5 font-mono text-ui-10-5',
                                    contextValue === n ? 'border-accent bg-accent-soft text-accent' : 'border-line text-muted hover:border-line-strong hover:bg-surface-2',
                                )}
                            >
                                {n / 1024}k
                            </button>
                        ))}
                        <button
                            type="button"
                            onClick={() => setForm({...form, contextTokens: ''})}
                            title={fmt(p.contextDefaultHint, {n: DEFAULT_OLLAMA_CONTEXT.toLocaleString()})}
                            className={cn(
                                'no-drag rounded border px-1.5 py-0.5 text-ui-10-5',
                                contextValue === 0 ? 'border-accent bg-accent-soft text-accent' : 'border-line text-muted hover:border-line-strong hover:bg-surface-2',
                            )}
                        >
                            {p.contextDefault}
                        </button>
                    </div>
                </div>
            ) : null}

            {probe && probe.state !== 'testing' ? <ProbeResult probe={probe} /> : null}

            {probeFailed ? (
                <p className="flex items-start gap-1.5 text-ui-11 text-faint">
                    <Info className="icon-ui-14 mt-px shrink-0" />
                    {p.saveAnywayNote}
                </p>
            ) : null}

            {showRemoteHint ? (
                <div className="flex flex-col gap-1 rounded-md border border-warn/50 bg-warn/15 px-2.5 py-2 text-ui-11 text-muted">
                    <p className="flex items-center gap-1.5 font-medium text-fg">
                        <Lightbulb className="icon-ui-14 shrink-0 text-warn" />
                        {p.remoteHintTitle}
                    </p>
                    <p>{p.remoteHintBody}</p>
                </div>
            ) : null}

            {error ? <p className="selectable rounded-md border border-danger/40 bg-danger/10 px-2 py-1.5 text-ui-11-5 text-danger">{error}</p> : null}

            <div className="flex items-center justify-end gap-2">
                <Button
                    onClick={() => void testForm()}
                    disabled={form.baseUrl.trim() === '' || keyMissing || probe?.state === 'testing'}
                    title={form.baseUrl.trim() === '' ? p.testNeedsUrl : keyMissing ? p.testNeedsKey : p.testNowHint}
                    className="mr-auto"
                >
                    <Plug className="icon-ui-14" />
                    {probe?.state === 'testing' ? p.testing : p.testNow}
                </Button>
                <Button variant="ghost" onClick={onCancel} title={p.cancelHint}>
                    {p.cancel}
                </Button>
                <Button
                    variant={probeFailed ? 'outline' : 'primary'}
                    onClick={() => void save()}
                    disabled={!formReady || saving}
                    title={formReady ? (probeFailed ? p.saveAnywayHint : p.saveHint) : keyMissing && form.name.trim() !== '' && form.baseUrl.trim() !== '' ? p.saveNeedsKey : p.saveNeedsFields}
                >
                    {probeFailed ? p.saveAnyway : p.save}
                </Button>
            </div>
        </div>
    )
}
