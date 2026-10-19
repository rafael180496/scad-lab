import {main} from '../../../../wailsjs/go/models'
import {useT} from '../../../i18n'
import {ModelPicker} from '../../chat/ModelPicker'
import {Input} from '../../ui/Field'

// Campo «Modelo por defecto» de un servidor. Con la lista de modelos a mano (la
// devolvió la prueba de conexión) se elige de ahí con búsqueda, que evita el
// error de tipeo en un nombre como `claude-sonnet-5-5`; sin lista se escribe a
// mano. Vacío significa «que elija el primero que liste el servidor».
export function DefaultModelField({
    value,
    onChange,
    modelIds,
    placeholder,
}: {
    value: string
    onChange: (v: string) => void
    modelIds: string[]
    placeholder?: string
}) {
    const t = useT().providers
    if (modelIds.length === 0) {
        return (
            <Input
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder={placeholder ?? t.defaultModelPlaceholder}
                title={t.defaultModelTitle}
                aria-label={t.defaultModel}
                className="font-mono text-ui-12"
            />
        )
    }
    return (
        <ModelPicker
            models={modelIds.map((id) => main.ModelInfo.createFrom({id}))}
            value={value}
            onChange={onChange}
            defaultHint={t.defaultModelEmpty}
            loading={false}
            error=""
            cli={false}
            align="start"
            className="w-full"
        />
    )
}
