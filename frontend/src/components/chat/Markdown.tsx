import {memo, useMemo, type ReactNode} from 'react'
import {Square, SquareCheck} from 'lucide-react'
import {BrowserOpenURL} from '../../../wailsjs/runtime/runtime'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {CodeBlock, type CodeActions} from './CodeBlock'
import {parseInline, parseMarkdown, type Block, type Inline} from './markdownParse'

// Render de markdown a elementos de React. Nunca se inyecta HTML: lo que dice
// un modelo es texto no confiable, y React escapa todo lo que no sea un nodo
// que este archivo crea a propósito. Los enlaces solo salen si son http(s) (el
// parser ya lo exige) y se abren en el navegador del sistema: dejar que la
// webview navegue reemplazaría la app entera por esa página.

function Inlines({nodes}: {nodes: Inline[]}): ReactNode {
    return nodes.map((n, i) => {
        switch (n.type) {
            case 'text':
                return n.text
            case 'br':
                return <br key={i} />
            case 'code':
                return (
                    <code key={i} className="rounded bg-surface-2 px-1 py-px font-mono text-[0.92em] text-fg">
                        {n.text}
                    </code>
                )
            case 'strong':
                return (
                    <strong key={i} className="font-semibold text-fg">
                        <Inlines nodes={n.children} />
                    </strong>
                )
            case 'em':
                return (
                    <em key={i}>
                        <Inlines nodes={n.children} />
                    </em>
                )
            case 'del':
                return (
                    <del key={i} className="text-muted decoration-faint">
                        <Inlines nodes={n.children} />
                    </del>
                )
            case 'link':
                return (
                    <a
                        key={i}
                        href={n.href}
                        title={n.href}
                        onClick={(e) => {
                            e.preventDefault()
                            BrowserOpenURL(n.href)
                        }}
                        className="text-info underline decoration-info/40 underline-offset-2 hover:decoration-info"
                    >
                        <Inlines nodes={n.children} />
                    </a>
                )
        }
    })
}

const Text = ({text}: {text: string}) => <Inlines nodes={parseInline(text)} />

const ALIGN = {left: 'text-left', center: 'text-center', right: 'text-right'}

// Relativos al cuerpo del chat (--chat-font-size): la jerarquía se conserva al cambiar la letra.
const headingCls = ['', 'text-[1.15em]', 'text-[1.08em]', 'text-[1em]', 'text-[1em]', 'text-[0.96em]', 'text-[0.96em]']

function BlockViewImpl({block, actions}: {block: Block; actions?: CodeActions}) {
    const t = useT()
    switch (block.type) {
        case 'code':
            return <CodeBlock lang={block.lang} code={block.code} closed={block.closed} actions={actions} />
        case 'heading':
            return (
                <p role="heading" aria-level={block.level} className={cn('mt-3 mb-1 font-semibold text-fg first:mt-0', headingCls[block.level], block.level <= 2 && 'border-b border-line pb-1')}>
                    <Text text={block.text} />
                </p>
            )
        case 'hr':
            return <hr className="my-3 border-line" />
        case 'quote':
            return (
                <blockquote className="my-2 border-l-2 border-line-strong pl-3 text-muted">
                    <Text text={block.text} />
                </blockquote>
            )
        case 'list':
            return (
                <ul className="my-1.5 flex flex-col gap-0.5">
                    {block.items.map((it, i) => (
                        <li key={i} className="flex gap-2" style={{paddingLeft: it.depth * 16}}>
                            {it.task ? (
                                <span className="flex w-4 shrink-0 justify-end pt-[0.2em]" aria-label={it.task === 'done' ? t.chat.task.done : t.chat.task.open}>
                                    {it.task === 'done' ? <SquareCheck className="icon-ui-14 text-ok" /> : <Square className="icon-ui-14 text-faint" />}
                                </span>
                            ) : (
                                <span className="w-4 shrink-0 text-right font-mono text-chat-sm text-faint">{it.marker}</span>
                            )}
                            <span className="min-w-0 flex-1">
                                <Text text={it.text} />
                            </span>
                        </li>
                    ))}
                </ul>
            )
        case 'table':
            return (
                <div className="my-2 overflow-x-auto rounded-lg border border-line">
                    <table className="w-full border-collapse text-chat-sm">
                        <thead className="bg-surface-2 text-muted">
                            <tr>
                                {block.head.map((h, i) => (
                                    <th key={i} className={cn('border-b border-line px-2.5 py-1 font-medium', ALIGN[block.align[i] ?? 'left'])}>
                                        <Text text={h} />
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {block.rows.map((r, i) => (
                                <tr key={i} className="border-b border-line last:border-0">
                                    {r.map((c, j) => (
                                        <td key={j} className={cn('px-2.5 py-1 align-top', ALIGN[block.align[j] ?? 'left'])}>
                                            <Text text={c} />
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )
        case 'paragraph':
            return (
                <p className="my-1.5 first:mt-0 last:mb-0">
                    <Text text={block.text} />
                </p>
            )
    }
}

// Cada bloque se memoiza por su texto: mientras llega una respuesta solo
// cambia el último, y los anteriores —un bloque de código de 80 líneas
// incluido— no se vuelven a pintar con cada trozo.
const BlockView = memo(BlockViewImpl, (a, b) => a.block.raw === b.block.raw && a.actions === b.actions)

export function Markdown({source, actions}: {source: string; actions?: CodeActions}) {
    const blocks = useMemo(() => parseMarkdown(source), [source])
    return (
        <div className="selectable text-chat leading-relaxed break-words text-fg">
            {blocks.map((b, i) => (
                <BlockView key={i} block={b} actions={actions} />
            ))}
        </div>
    )
}
