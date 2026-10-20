import {Bot, FileClock, FileDiff, Undo2} from 'lucide-react'
import {Button, IconButton} from '../ui/Button'
import {openDiff, openHistory} from '../../lib/historyUI'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {undoAIChange} from './historyActions'

// «Editado por la IA» con sus acciones (S16): la cabecera del editor y la del visor del checkpoint
// elegido. El indicador, «Deshacer cambio de la IA» y «Ver diferencias» solo aparecen si la última
// versión del archivo la dejó la IA; «Historial» está siempre (las versiones restauradas también
// viven ahí). `compact` deja solo íconos, con el `title` como nombre.

/** Indicador de un nodo o de una cabecera: la IA editó este archivo en el lugar. */
export function EditedByAIBadge({file, className}: {file: string; className?: string}) {
    const t = useT()
    return (
        <span
            className={cn('flex shrink-0 items-center gap-1 rounded bg-accent-soft px-1 py-px text-accent', className)}
            title={fmt(t.history.editedByAIFile, {file}) + '\n' + t.history.editedByAIHint}
            aria-label={t.history.editedByAI}
        >
            <Bot className="icon-ui-12" />
        </span>
    )
}

export function HistoryControls({workspaceId, file, editedByAI, compact}: {workspaceId: number; file: string; editedByAI: boolean; compact: boolean}) {
    const t = useT()
    const h = t.history
    const small = 'h-6 px-2 text-ui-11-5'
    return (
        <>
            {editedByAI ? (
                <>
                    <span
                        className="mr-1 flex shrink-0 items-center gap-1 rounded bg-accent-soft px-1.5 py-px text-ui-11 text-accent"
                        title={h.editedByAIHint}
                    >
                        <Bot className="icon-ui-12" />
                        {compact ? null : h.editedByAI}
                    </span>
                    <Button className={small} title={fmt(h.undoHint, {file})} onClick={() => void undoAIChange(workspaceId, file, t)}>
                        <Undo2 className="icon-ui-12" />
                        {compact ? null : h.undo}
                    </Button>
                    <IconButton title={fmt(h.diffHint, {file})} className="size-6" onClick={() => openDiff(workspaceId, file)}>
                        <FileDiff className="icon-ui-14" />
                    </IconButton>
                </>
            ) : null}
            <IconButton title={fmt(h.openHint, {file})} className="size-6" onClick={() => openHistory(workspaceId, file)}>
                <FileClock className="icon-ui-14" />
            </IconButton>
        </>
    )
}
