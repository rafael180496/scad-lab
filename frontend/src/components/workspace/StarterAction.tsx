import {useState} from 'react'
import {ImagePlus, Sparkles} from 'lucide-react'
import {CreateStarterCheckpoint} from '../../../wailsjs/go/main/App'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {FromImageDialog} from '../chat/FromImageDialog'
import {Button} from '../ui/Button'
import {toast} from '../ui/Toast'

// «Crear un modelo de inicio» (S12.c). Al abrir, un workspace sin ningún .scad recibe solo el modelo de
// inicio; este botón es para el que quedó vacío a propósito (su papelera guarda algo, así que no se
// resucita solo) y aparece en el visor y en Código cuando el proyecto no tiene ningún checkpoint.
// Junto a él, «Crear desde una imagen» (S15): el otro punto de partida de un proyecto vacío.
export function StarterAction({workspaceId, onCreated, className}: {workspaceId: number | null; onCreated: (file: string) => void; className?: string}) {
    const t = useT()
    const [busy, setBusy] = useState(false)
    const [fromImage, setFromImage] = useState(false)
    if (workspaceId == null) return null
    const create = async () => {
        setBusy(true)
        try {
            const file = await CreateStarterCheckpoint(workspaceId)
            if (file) {
                toast.ok(fmt(t.starter.created, {file}))
                onCreated(file)
            }
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }
    return (
        <div className={className ?? 'mt-3 flex flex-col items-center gap-2'}>
            <p className="text-ui-12 text-faint">{t.starter.text}</p>
            <div className="flex flex-wrap items-center justify-center gap-2">
                <Button variant="primary" data-testid="create-starter" title={t.starter.hint} disabled={busy} onClick={() => void create()}>
                    <Sparkles className="icon-ui-14" />
                    {t.starter.button}
                </Button>
                <Button data-testid="create-from-image" title={t.fromImage.hint} onClick={() => setFromImage(true)}>
                    <ImagePlus className="icon-ui-14" />
                    {t.fromImage.button}
                </Button>
            </div>
            <FromImageDialog open={fromImage} workspaceId={workspaceId} onClose={() => setFromImage(false)} />
        </div>
    )
}
