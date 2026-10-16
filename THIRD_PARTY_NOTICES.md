# Avisos de terceros / Third-party notices

ScadLab se distribuye con licencia MIT (ver [LICENSE](LICENSE)). El binario de
ScadLab incluye, además de su propio código, los componentes que se listan acá.
Cada uno conserva su licencia.

ScadLab is released under the MIT license ([LICENSE](LICENSE)). The ScadLab
binary also embeds the components listed below; each keeps its own license.

---

## 1. OpenSCAD compilado a WebAssembly (motor integrado)

| | |
|---|---|
| Qué es | El programa OpenSCAD oficial, compilado a WebAssembly (`openscad.wasm`). Es el motor de respaldo que ScadLab usa cuando no encuentra un OpenSCAD instalado. |
| Versión | `2025.12.30.wasm30277` (snapshot de openscad.org; `openscad --version` imprime esa cadena) |
| Origen | <https://files.openscad.org/snapshots/OpenSCAD-2025.12.30.wasm30277-WebAssembly-web.zip> |
| SHA-256 del zip | `8b3b20dfd91d2e949d9e8dea7f8aca9917fc415a9d9d8190f706e19721b23f33` |
| SHA-256 de `openscad.wasm` | `1db0bee42c5cf7e4a72208f7d85fc53c12be0980fd98b9f1dbd5e9dcdc732f72` |
| Código fuente correspondiente | <https://github.com/openscad/openscad/tree/5fce99a7bdd3c2190977d509dd46d773c2f28300> (commit `5fce99a7bdd3c2190977d509dd46d773c2f28300`, 2025-12-30; es el que el propio programa informa como `git 5fce99a7`) |
| Licencia | GNU General Public License, versión 2 o posterior. Con CGAL enlazado (GPL v3 o posterior) el programa combinado se rige por la GPL v3 o posterior. Texto: <https://www.gnu.org/licenses/gpl-3.0.html> |
| Dónde está en este repo | `assets/engines/openscad-wasm/openscad.wasm.gz` |

**Qué se distribuye.** El archivo `openscad.wasm` es el publicado por
openscad.org, **sin ninguna modificación**; en el repo y en el binario solo está
comprimido con gzip. Al arrancar, ScadLab lo descomprime en memoria y **verifica
que su SHA-256 sea el de la tabla de arriba antes de ejecutarlo** (si no
coincide, no se ejecuta).

**Por qué no hace de ScadLab una obra derivada.** ScadLab (MIT) no enlaza ni
incorpora código de OpenSCAD en su propio código: ejecuta el `.wasm` como un
programa aparte, pasándole argumentos de línea de comandos y archivos, y leyendo
su salida —exactamente lo que hace cuando lanza un OpenSCAD instalado con `exec`—.
Reunir los dos en el mismo binario es una *agregación* de programas separados
(GPL v2 §2, último párrafo; GPL v3 §5, «aggregate»), no una obra combinada. Si
modificas `openscad.wasm` o lo redistribuyes por separado, te rigen las
condiciones de la GPL para ese archivo.

**Cómo obtener el código fuente.** El del commit indicado, en el enlace de la
tabla. Las bibliotecas que OpenSCAD enlaza dentro del `.wasm` (CGAL, Manifold,
Clipper2, Boost, Eigen, FreeType, HarfBuzz, fontconfig, libzip, lib3mf, GLib,
lodepng, entre otras, y las bibliotecas de ejecución de Emscripten 4.0.10) están
en el árbol de OpenSCAD o se construyen con sus scripts; cada una con su
licencia, que OpenSCAD documenta en su `COPYING` y en su página de licencias.
`openscad.wasm --info` (se puede ejecutar con ScadLab) lista las versiones
exactas.

**OpenSCAD instalado o descargado.** El OpenSCAD que el usuario instala por su
cuenta, o que ScadLab le descarga desde openscad.org cuando él lo pide
(Configuración → OpenSCAD), **no forma parte** de ScadLab ni de este binario: lo
baja el usuario de openscad.org, con la verificación de su SHA-256.

## 2. Liberation Fonts 2.1.5 (fuentes de `text()` del motor integrado)

| | |
|---|---|
| Qué es | Liberation Sans (Regular, Bold, Italic, Bold Italic). `text()` de OpenSCAD usa «Liberation Sans» por defecto. |
| Versión | 2.1.5 |
| Origen | <https://github.com/liberationfonts/liberation-fonts/files/7261482/liberation-fonts-ttf-2.1.5.tar.gz> (SHA-256 del `.tar.gz`: `7191c669bf38899f73a2094ed00f7b800553364f90e2637010a69c0e268f25d0`) · proyecto: <https://github.com/liberationfonts/liberation-fonts> |
| Copyright | © 2010 Google Corporation (con las Reserved Font Names Arimo, Tinos y Cousine); © 2012 Red Hat, Inc. (con la Reserved Font Name Liberation) |
| Licencia | SIL Open Font License, versión 1.1 — <https://openfontlicense.org> |
| Dónde está en este repo | `assets/engines/openscad-wasm/fonts/` (los `.ttf` sin modificar y el texto de la licencia en `LICENSE-Liberation-OFL`) |

Las fuentes se embeben sin modificar y se montan de solo lectura en `/fonts`
dentro del motor integrado. Se permite agruparlas y redistribuirlas con software
de cualquier licencia siempre que no se vendan solas y viajen con su licencia, que
es lo que hace este repo. `fonts.conf`, junto a ellas, es un archivo de
configuración de ScadLab (MIT), no parte de las fuentes.

## 3. wazero v1.12.0 (ejecuta el WebAssembly)

| | |
|---|---|
| Qué es | Runtime de WebAssembly en Go puro (sin CGO) con el que ScadLab ejecuta `openscad.wasm`. |
| Versión | v1.12.0 |
| Origen | <https://github.com/tetratelabs/wazero> |
| Licencia | Apache License 2.0 — <https://www.apache.org/licenses/LICENSE-2.0> |
| Copyright | © 2020-2023 wazero authors |

wazero se enlaza como biblioteca Go (`github.com/tetratelabs/wazero`) y se
distribuye sin cambios.

## 4. Otras dependencias

Las demás dependencias de Go de ScadLab (Wails, modernc.org/sqlite, fsnotify,
go-pty, el SDK de Anthropic, etc.) y las del frontend se listan en `go.mod` y
`frontend/package.json`, cada una con su licencia permisiva en su repositorio.
