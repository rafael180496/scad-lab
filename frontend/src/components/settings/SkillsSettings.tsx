import {useCallback, useEffect, useRef, useState} from 'react'
import {Eye, FolderInput, FolderOpen, Pencil, Plus, TextCursorInput, Trash2} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import {
    ChangeSkillsFolder,
    ChooseDirectory,
    CreateSkill,
    DeleteSkill,
    GetSkillsFolder,
    ListSkills,
    OpenSkillsFolder,
    RenameSkill,
    SetSkillEnabled,
} from '../../../wailsjs/go/main/App'
import {Button, IconButton} from '../ui/Button'
import {confirmDialog, Dialog} from '../ui/Dialog'
import {Field, Input, Toggle} from '../ui/Field'
import {toast} from '../ui/Toast'
import {errorText} from '../../lib/errors'
import {skillsShot} from '../../lib/skillsShot'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {SkillEditor} from './SkillEditor'

// Mismo patrón que valida Go (skills.CheckName): kebab-case de 2 a 63 caracteres.
const NAME_RE = /^[a-z0-9][a-z0-9-]{1,62}$/

/**
 * Configuración → Skills: la lista de skills (incluidas y propias) con sus
 * acciones, la carpeta de las skills propias y el alta de una nueva. Todo lo de
 * esta pestaña se aplica al instante: no pasa por «Guardar» del diálogo.
 */
export function SkillsSettings({fontSize, tabSize, wordWrap}: {fontSize: number; tabSize: number; wordWrap: boolean}) {
    const t = useT()
    const k = t.skills
    const [skills, setSkills] = useState<main.SkillView[]>([])
    const [folder, setFolder] = useState<main.SkillsFolder | null>(null)
    const [newSkill, setNewSkill] = useState('')
    const [editing, setEditing] = useState<{name: string; readOnly: boolean} | null>(null)
    const [renaming, setRenaming] = useState<main.SkillView | null>(null)
    const [renameTo, setRenameTo] = useState('')
    // Carpeta elegida que espera decisión (mover o solo apuntar). '' = la predeterminada.
    const [pendingFolder, setPendingFolder] = useState<string | null>(null)

    const reload = useCallback(async () => {
        const [list, f] = await Promise.all([ListSkills(), GetSkillsFolder()])
        setSkills(list ?? [])
        setFolder(f)
        return list ?? []
    }, [])

    useEffect(() => {
        reload().catch((e) => toast.error(errorText(e)))
    }, [reload])

    // ── Eliminar ────────────────────────────────────────────────────────────
    const askDelete = useCallback(
        async (s: main.SkillView) => {
            if (!(await confirmDialog(fmt(k.deleteConfirm, {name: s.name, dir: s.dir}), k.deleteOk, k.deleteCancel, {danger: true}))) return
            try {
                const r = await DeleteSkill(s.name)
                toast.ok(fmt(k.deleted, {name: s.name, trash: r.trash}))
                if (r.kept?.length) toast.error(fmt(k.deletedKept, {n: r.kept.length, paths: r.kept.join(', ')}))
                if (r.syncError) toast.error(fmt(k.syncFailed, {msg: r.syncError}))
            } catch (e) {
                toast.error(errorText(e))
            } finally {
                await reload().catch(() => {})
            }
        },
        [k, reload],
    )

    // ── Renombrar ───────────────────────────────────────────────────────────
    const doRename = async () => {
        if (!renaming) return
        const to = renameTo.trim()
        try {
            const r = await RenameSkill(renaming.name, to)
            toast.ok(fmt(k.renamed, {name: r.name}))
            if (r.kept?.length) toast.error(fmt(k.renamedKept, {n: r.kept.length, paths: r.kept.join(', ')}))
            if (r.syncError) toast.error(fmt(k.syncFailed, {msg: r.syncError}))
            setRenaming(null)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            await reload().catch(() => {})
        }
    }

    // ── Carpeta de las skills propias ───────────────────────────────────────
    const applyFolder = async (dir: string, move: boolean) => {
        setPendingFolder(null)
        try {
            const r = await ChangeSkillsFolder(dir, move)
            const moved = r.moved?.length ?? 0
            toast.ok(moved === 1 ? fmt(k.movedOne, {path: r.path}) : moved > 1 ? fmt(k.movedMany, {n: moved, path: r.path}) : fmt(k.changed, {path: r.path}))
            if (r.skipped?.length) toast.error(fmt(k.skipped, {names: r.skipped.join(', ')}))
            if (r.syncError) toast.error(fmt(k.syncFailed, {msg: r.syncError}))
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            await reload().catch(() => {})
        }
    }

    // Con skills propias que mover se pregunta; sin ellas no hay nada que decidir.
    const chooseFolder = async (dir: string) => {
        if (folder && folder.count > 0) setPendingFolder(dir)
        else await applyFolder(dir, false)
    }

    const pickFolder = async () => {
        if (!folder) return
        try {
            const dir = await ChooseDirectory(k.chooseTitle, folder.path)
            if (dir) await chooseFolder(dir)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const openFolder = async () => {
        try {
            await OpenSkillsFolder()
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            // Si no existía, el botón la acaba de crear.
            await reload().catch(() => {})
        }
    }

    // ── Crear ───────────────────────────────────────────────────────────────
    const create = async () => {
        const name = newSkill.trim()
        if (!name) return
        try {
            await CreateSkill(name)
            setNewSkill('')
            const list = await reload()
            // La skill nace con la descripción pendiente: se abre su editor para escribirla.
            if (list.some((s) => s.name === name)) setEditing({name, readOnly: false})
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // ── Banco de capturas ───────────────────────────────────────────────────
    const shotDone = useRef(false)
    useEffect(() => {
        const mode = skillsShot.mode
        if (!mode || shotDone.current || skills.length === 0 || !folder) return
        shotDone.current = true
        const own = skills.find((s) => s.name === 'gear-helper')
        const builtin = skills.find((s) => s.name === 'openscad-core')
        if ((mode === 'editor' || mode === 'editor-errors') && own) setEditing({name: own.name, readOnly: false})
        if (mode === 'editor-builtin' && builtin) setEditing({name: builtin.name, readOnly: true})
        if (mode === 'delete' && own) void askDelete(own)
        if (mode === 'folder') setPendingFolder('/Users/maker/Documents/Taller 3D/skills')
        if (mode === 'rename' && own) {
            setRenaming(own)
            setRenameTo(own.name)
        }
    }, [skills, folder, askDelete])

    const editingSkill = editing ? (skills.find((s) => s.name === editing.name) ?? null) : null
    const renameValid = NAME_RE.test(renameTo.trim()) && renameTo.trim() !== renaming?.name

    return (
        <div className="flex flex-col gap-4">
            <p className="text-ui-12 leading-relaxed text-muted">{t.settings.skillsIntro}</p>
            <div className="divide-y divide-line rounded-lg border border-line">
                {skills.map((s) => {
                    const own = s.source === 'user'
                    return (
                        <div key={s.name} className="flex items-start gap-3 px-3 py-2.5">
                            <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-2">
                                    <span className="truncate font-mono text-ui-12 font-medium">{s.name}</span>
                                    <span className={cn('shrink-0 rounded px-1.5 py-px font-mono text-ui-10', own ? 'bg-accent-soft text-accent' : 'bg-surface-2 text-faint')}>
                                        {own ? t.settings.user : t.settings.builtin}
                                    </span>
                                </div>
                                <p className="mt-0.5 line-clamp-2 text-ui-11-5 leading-snug text-faint">{s.description}</p>
                            </div>
                            <div className="flex shrink-0 items-center justify-end gap-1">
                                {own ? (
                                    <>
                                        <Button className="h-6 px-2 text-ui-11-5" title={fmt(k.editHint, {name: s.name})} onClick={() => setEditing({name: s.name, readOnly: false})}>
                                            <Pencil className="icon-ui-12" />
                                            {k.edit}
                                        </Button>
                                        <IconButton
                                            title={fmt(k.renameHint, {name: s.name})}
                                            className="size-6"
                                            onClick={() => {
                                                setRenaming(s)
                                                setRenameTo(s.name)
                                            }}
                                        >
                                            <TextCursorInput className="icon-ui-14" />
                                        </IconButton>
                                        <IconButton title={fmt(k.deleteHint, {name: s.name})} className="size-6 hover:text-danger" onClick={() => void askDelete(s)}>
                                            <Trash2 className="icon-ui-14" />
                                        </IconButton>
                                    </>
                                ) : (
                                    <Button className="h-6 px-2 text-ui-11-5" title={fmt(k.viewHint, {name: s.name})} onClick={() => setEditing({name: s.name, readOnly: true})}>
                                        <Eye className="icon-ui-12" />
                                        {k.view}
                                    </Button>
                                )}
                                <span className="ml-1.5 flex">
                                    <Toggle
                                        checked={s.enabled}
                                        title={own ? t.settings.enabled : k.builtinToggleHint}
                                        onChange={async (v) => {
                                            setSkills((l) => l.map((x) => (x.name === s.name ? {...x, enabled: v} : x)) as main.SkillView[])
                                            try {
                                                await SetSkillEnabled(s.name, v)
                                            } catch (e) {
                                                toast.error(errorText(e))
                                            }
                                        }}
                                    />
                                </span>
                            </div>
                        </div>
                    )
                })}
            </div>

            {/* Dónde viven las skills propias */}
            <section className="rounded-lg border border-line bg-surface-2 p-3">
                <div className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.folderLabel}</div>
                <div className="mt-1.5 flex items-center gap-2">
                    <span className="selectable min-w-0 flex-1 truncate font-mono text-ui-12" title={folder?.path}>
                        {folder?.path ?? ''}
                    </span>
                    <Button title={k.changeHint} onClick={() => void pickFolder()}>
                        <FolderInput className="icon-ui-14" />
                        {k.change}
                    </Button>
                    <Button title={k.openFolderHint} onClick={() => void openFolder()}>
                        <FolderOpen className="icon-ui-14" />
                        {k.openFolder}
                    </Button>
                    {folder?.custom ? (
                        <Button variant="ghost" title={fmt(k.resetHint, {path: folder.default})} onClick={() => void chooseFolder('')}>
                            {k.reset}
                        </Button>
                    ) : null}
                </div>
                {folder && !folder.exists ? <p className="mt-1.5 text-ui-11-5 text-faint">{k.folderMissing}</p> : null}
            </section>

            <Field label={t.settings.newSkill} hint={t.settings.newSkillHint}>
                <div className="flex gap-2">
                    <Input
                        className="font-mono"
                        value={newSkill}
                        placeholder={t.settings.newSkillPlaceholder}
                        title={t.settings.newSkillHint}
                        onChange={(e) => setNewSkill(e.target.value.toLowerCase())}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') void create()
                        }}
                    />
                    <Button variant="primary" title={t.settings.newSkillHint} disabled={!newSkill.trim()} onClick={() => void create()}>
                        <Plus className="icon-ui-14" />
                        {t.settings.newSkill}
                    </Button>
                </div>
            </Field>

            <SkillEditor
                skill={editingSkill}
                readOnly={editing?.readOnly ?? false}
                onClose={() => setEditing(null)}
                onChanged={() => void reload().catch(() => {})}
                fontSize={fontSize}
                tabSize={tabSize}
                wordWrap={wordWrap}
                shot={skillsShot.mode === 'editor-errors' ? {invalid: true} : undefined}
            />

            {/* Renombrar */}
            <Dialog open={renaming != null} onClose={() => setRenaming(null)} title={k.renameTitle} closeLabel={k.renameCancel} className="w-[460px]"
                footer={
                    <>
                        <Button title={k.renameCancel} onClick={() => setRenaming(null)}>
                            {k.renameCancel}
                        </Button>
                        <Button variant="primary" title={k.renameOk} disabled={!renameValid} onClick={() => void doRename()}>
                            {k.renameOk}
                        </Button>
                    </>
                }
            >
                <div className="flex flex-col gap-2 px-4 py-4">
                    <Field label={k.renameLabel} hint={renameTo.trim() && !NAME_RE.test(renameTo.trim()) ? <span className="text-danger">{k.renameInvalid}</span> : k.renameFieldHint}>
                        <Input
                            data-autofocus
                            className="font-mono"
                            value={renameTo}
                            title={k.renameLabel}
                            onChange={(e) => setRenameTo(e.target.value.toLowerCase())}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter' && renameValid) void doRename()
                            }}
                        />
                    </Field>
                </div>
            </Dialog>

            {/* Cambiar la carpeta con skills propias: mover o solo apuntar */}
            <Dialog open={pendingFolder != null} onClose={() => setPendingFolder(null)} title={k.move.title} closeLabel={k.move.cancel} className="w-[560px]"
                footer={
                    <>
                        <Button title={k.move.cancel} onClick={() => setPendingFolder(null)}>
                            {k.move.cancel}
                        </Button>
                        <Button title={k.move.pointHint} onClick={() => void applyFolder(pendingFolder ?? '', false)}>
                            {k.move.point}
                        </Button>
                        <Button variant="primary" title={k.move.actionHint} onClick={() => void applyFolder(pendingFolder ?? '', true)}>
                            {k.move.action}
                        </Button>
                    </>
                }
            >
                <div className="flex flex-col gap-3 px-4 py-4">
                    <p className="text-ui-13 leading-relaxed text-muted">{folder?.count === 1 ? k.move.bodyOne : fmt(k.move.bodyMany, {n: folder?.count ?? 0})}</p>
                    <dl className="grid grid-cols-[64px_1fr] gap-x-3 gap-y-1.5 text-ui-12">
                        <dt className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.move.now}</dt>
                        <dd className="selectable font-mono break-all">{folder?.path}</dd>
                        <dt className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.move.next}</dt>
                        <dd className="selectable font-mono break-all">{pendingFolder === '' ? folder?.default : pendingFolder}</dd>
                    </dl>
                    <ul className="flex flex-col gap-1.5 text-ui-11-5 leading-snug text-faint">
                        <li>{k.move.explainMove}</li>
                        <li>{k.move.explainPoint}</li>
                    </ul>
                </div>
            </Dialog>
        </div>
    )
}
