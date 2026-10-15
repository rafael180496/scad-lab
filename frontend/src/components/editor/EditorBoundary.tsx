import {Component, type ReactNode} from 'react'

// Si Monaco no carga (chunk roto, WebView sin workers…), el panel muestra el
// código como texto en vez de dejar la ventana entera en blanco.
export class EditorBoundary extends Component<{fallback: (error: string) => ReactNode; resetKey: string; children: ReactNode}, {error: string}> {
    state = {error: ''}

    static getDerivedStateFromError(e: unknown) {
        return {error: e instanceof Error ? e.message : String(e)}
    }

    componentDidUpdate(prev: {resetKey: string}) {
        if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({error: ''})
    }

    render() {
        return this.state.error ? this.props.fallback(this.state.error) : this.props.children
    }
}
