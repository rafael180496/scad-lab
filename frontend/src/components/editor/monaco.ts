// Monaco empaquetado local (regla 6 de .claude/rules/technical.md: nada de red
// en runtime). Se importa el núcleo (`editor.api`) y solo las contribuciones
// que usa el editor de ScadLab, no `monaco-editor` entero: el paquete completo
// trae 80 lenguajes y los servicios de TS/JSON/CSS/HTML que acá no sirven.
//
// Este módulo vive en el chunk del editor (entra con React.lazy): la app abre
// sin cargarlo.
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import 'monaco-editor/esm/vs/features/codeEditor/register.js'
import 'monaco-editor/esm/vs/features/codicon/register.js'
import 'monaco-editor/esm/vs/features/bracketMatching/register.js'
import 'monaco-editor/esm/vs/features/caretOperations/register.js'
import 'monaco-editor/esm/vs/features/clipboard/register.js'
import 'monaco-editor/esm/vs/features/comment/register.js'
import 'monaco-editor/esm/vs/features/contextmenu/register.js'
import 'monaco-editor/esm/vs/features/cursorUndo/register.js'
import 'monaco-editor/esm/vs/features/find/register.js'
import 'monaco-editor/esm/vs/features/folding/register.js'
import 'monaco-editor/esm/vs/features/format/register.js'
import 'monaco-editor/esm/vs/features/gotoError/register.js'
import 'monaco-editor/esm/vs/features/gotoLine/register.js'
import 'monaco-editor/esm/vs/features/hover/register.js'
import 'monaco-editor/esm/vs/features/indentation/register.js'
import 'monaco-editor/esm/vs/features/linesOperations/register.js'
import 'monaco-editor/esm/vs/features/multicursor/register.js'
import 'monaco-editor/esm/vs/features/parameterHints/register.js'
import 'monaco-editor/esm/vs/features/readOnlyMessage/register.js'
import 'monaco-editor/esm/vs/features/smartSelect/register.js'
import 'monaco-editor/esm/vs/features/snippet/register.js'
import 'monaco-editor/esm/vs/features/suggest/register.js'
// El feature «suggest» solo registra las sugerencias en línea; el widget de
// autocompletado (Ctrl+Espacio) vive en el controlador, que el paquete
// completo trae de rebote por inlineCompletions.
import 'monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController.js'
import 'monaco-editor/esm/vs/features/wordHighlighter/register.js'
import 'monaco-editor/esm/vs/features/wordOperations/register.js'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker.js?worker'
import {FormatScad} from '../../../wailsjs/go/main/App'
import {errorText} from '../../lib/errors'
import {toast} from '../ui/Toast'
import {LANGUAGE_ID, languageConfiguration, monarch} from './scadLanguage'
import {registerCatalogProviders} from './catalog'

// Solo el worker base del editor (cálculo de ediciones mínimas al formatear,
// palabras del documento para sugerir). Los de TS/JSON/CSS no hacen falta.
self.MonacoEnvironment = {getWorker: () => new EditorWorker()}

monaco.languages.register({id: LANGUAGE_ID, extensions: ['.scad'], aliases: ['OpenSCAD', 'scad']})
monaco.languages.setLanguageConfiguration(LANGUAGE_ID, languageConfiguration)
monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, monarch([], []))
registerCatalogProviders(monaco, (modules, functions) => monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, monarch(modules, functions)))

// Formatear (Shift+Alt+F, el botón y el menú contextual) pasa por el
// formateador de Go (backend/scad). Monaco convierte el reemplazo entero en
// ediciones mínimas con su worker: el cursor y el scroll se quedan donde
// estaban y se deshace con un solo Cmd/Ctrl+Z.
monaco.languages.registerDocumentFormattingEditProvider(LANGUAGE_ID, {
    displayName: 'ScadLab',
    async provideDocumentFormattingEdits(model) {
        try {
            const text = await FormatScad(model.getValue())
            return [{range: model.getFullModelRange(), text}]
        } catch (e) {
            toast.error(errorText(e))
            return []
        }
    },
})

// ── Tema ────────────────────────────────────────────────────────────────────
// Sale de los tokens de index.css (nunca colores sueltos) y se vuelve a
// definir cuando cambia la clase de <html>: el editor sigue al tema de la app.
const THEME = 'scadlab'

function cssVar(css: CSSStyleDeclaration, name: string): string {
    const v = css.getPropertyValue(name).trim()
    return /^#[0-9a-fA-F]{6}$/.test(v) ? v : '#808080'
}

function defineTheme() {
    const css = getComputedStyle(document.documentElement)
    const c = (name: string) => cssVar(css, `--c-${name}`)
    const hex = (name: string) => c(name).slice(1)
    const dark = document.documentElement.classList.contains('dark')
    monaco.editor.defineTheme(THEME, {
        base: dark ? 'vs-dark' : 'vs',
        inherit: true,
        rules: [
            {token: '', foreground: hex('fg')},
            {token: 'keyword', foreground: hex('accent')},
            {token: 'keyword.directive', foreground: hex('accent')},
            {token: 'keyword.modifier', foreground: hex('danger'), fontStyle: 'bold'},
            {token: 'entity.name', foreground: hex('fg'), fontStyle: 'bold'},
            {token: 'predefined', foreground: hex('info')},
            {token: 'variable.special', foreground: hex('warn')},
            {token: 'constant', foreground: hex('warn')},
            {token: 'number', foreground: hex('warn')},
            {token: 'string', foreground: hex('ok')},
            {token: 'string.path', foreground: hex('ok')},
            {token: 'string.escape', foreground: hex('warn')},
            {token: 'string.escape.invalid', foreground: hex('danger')},
            {token: 'operator', foreground: hex('muted')},
            {token: 'delimiter', foreground: hex('muted')},
            {token: 'comment', foreground: hex('faint'), fontStyle: 'italic'},
            {token: 'comment.checkpoint', foreground: hex('muted'), fontStyle: 'italic'},
            {token: 'annotation.checkpoint', foreground: hex('accent'), fontStyle: 'bold italic'},
            {token: 'annotation.section', foreground: hex('info'), fontStyle: 'bold'},
            {token: 'annotation.range', foreground: hex('info'), fontStyle: 'italic'},
        ],
        colors: {
            'editor.background': c('surface'),
            'editor.foreground': c('fg'),
            'editorLineNumber.foreground': c('faint'),
            'editorLineNumber.activeForeground': c('fg'),
            'editor.lineHighlightBackground': c('surface-2'),
            'editor.lineHighlightBorder': c('surface-2'),
            'editor.selectionBackground': c('accent') + '40',
            'editor.inactiveSelectionBackground': c('accent') + '26',
            'editor.selectionHighlightBackground': c('accent') + '1f',
            'editor.wordHighlightBackground': c('info') + '26',
            'editor.findMatchBackground': c('accent') + '66',
            'editor.findMatchHighlightBackground': c('accent') + '33',
            'editorCursor.foreground': c('accent'),
            'editorIndentGuide.background1': c('line'),
            'editorIndentGuide.activeBackground1': c('line-strong'),
            'editorBracketMatch.background': c('accent') + '26',
            'editorBracketMatch.border': c('accent') + '80',
            'editorGutter.background': c('surface'),
            'editorError.foreground': c('danger'),
            'editorWarning.foreground': c('warn'),
            'editorInfo.foreground': c('info'),
            'editorOverviewRuler.border': c('line'),
            'editorWidget.background': c('raised'),
            'editorWidget.foreground': c('fg'),
            'editorWidget.border': c('line-strong'),
            'editorHoverWidget.background': c('raised'),
            'editorHoverWidget.border': c('line-strong'),
            'editorSuggestWidget.background': c('raised'),
            'editorSuggestWidget.border': c('line-strong'),
            'editorSuggestWidget.selectedBackground': c('accent-soft'),
            'editorSuggestWidget.highlightForeground': c('accent'),
            'input.background': c('surface-2'),
            'input.border': c('line'),
            'inputOption.activeBorder': c('accent'),
            'focusBorder': c('accent'),
            'scrollbarSlider.background': c('line-strong') + '99',
            'scrollbarSlider.hoverBackground': c('line-strong'),
            'scrollbarSlider.activeBackground': c('faint'),
            'minimap.background': c('surface'),
            'widget.shadow': '#00000040',
        },
    })
    monaco.editor.setTheme(THEME)
}

defineTheme()
new MutationObserver(defineTheme).observe(document.documentElement, {attributes: true, attributeFilter: ['class']})

export {monaco, THEME}
