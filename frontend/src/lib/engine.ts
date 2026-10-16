// El motor integrado informa su versión como «2025.12.30.wasm30277» (lo que
// imprime --version, con el número de build del WebAssembly). Para la interfaz
// basta la versión de OpenSCAD: «2025.12.30».
export function wasmShortVersion(version: string): string {
    return version.replace(/\.wasm\d+$/, '')
}
