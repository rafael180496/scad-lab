# Convenciones

## Commits / PRs — regla dura

**Solo el usuario hace commits.** Ni Claude ni los sub-agentes (`architect`,
`fullstack-dev`, `openscad-dev`) ejecutan `git add`, `git commit`, `git push`,
tags, `gh pr create` ni nada que escriba en el historial o en el remoto. Una
autorización dada en otra conversación no se extiende a esta. Al terminar, se
avisa qué archivos quedaron listos para que el usuario los revise y commitee.

La misma regla está en las reglas de cada workspace (`CLAUDE.md`/`AGENTS.md`):
los agentes que diseñan modelos tampoco commitean.

## Trabajo por fases

Todo cambio se ubica en una fase de `claude/01-roadmap.md`, se descompone en
tareas ordenadas antes de tocar código y se marca al terminar.

## CHANGELOG.md

[Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) + SemVer (`VERSION`).
Todo cambio visible suma una entrada bajo `## [Unreleased]`, en español, en
estilo narrativo (qué cambió y por qué le importa a quien usa la app). Nunca se
escribe en la sección de una versión ya publicada.

## index.html — la ayuda se actualiza con el mismo cambio

La ayuda pública es `index.html` (GitHub Pages). Un módulo nuevo es un grupo
nuevo en el índice; una funcionalidad con entidad propia es un
`<article class="topic">` nuevo; un detalle es un `<h2>` en el tema existente.
Lo planificado lleva `data-status="dev"` y la insignia «En desarrollo», que se
quita cuando la funcionalidad sale. La insignia de versión (`<span class="ver">`)
y el pie nombran la versión que documenta la página.

### Capturas de la ayuda

Misma regla que mini-tools:

- **Toda pantalla que la ayuda documenta lleva su captura**, y se actualiza en
  el **mismo cambio** que modifica esa pantalla. Un `<figure class="shot pending">`
  («Captura pendiente») solo es válido mientras la pantalla todavía no existe.
- **Las capturas salen del banco, nunca de la app real**: una vista nueva va en
  `VIEWS` de `frontend/src/uishot.tsx` con datos inventados (una captura real
  mete rutas, proyectos y nombres del usuario). `./scripts/uishot.sh <vista>`
  saca una sola, para revisar.
- **`./scripts/help-shots.sh [archivo]`** las regenera todas a partir de su
  tabla (archivo → vista → tamaño), en inglés en `docs/screenshots/` y en
  español en `docs/screenshots/es/` (la que usa `index.html`). Una captura nueva
  de la ayuda = una fila en esa tabla.
- Se **miran** antes de dar el cambio por terminado: una captura en blanco, a
  mitad de una transición o con datos de otro escenario es peor que ninguna.
- `<img>` con `alt` descriptivo (qué se ve, no «captura») y `<figcaption>` que
  diga qué mirar.

## Español de la interfaz y la ayuda: tú, no vos

Todo texto en español que ve el usuario —diccionario `es.ts`, mensajes
`i18n.Msg{ES}` del backend, `RENDER_ERRORS.md`, prompts de `assets/prompts`,
ayuda `index.html`, README— va en **español neutro con tú**: «Elige», «Crea»,
«puedes», «Pide», nunca «Elegí», «Creá», «podés», «Pedí». La mezcla aparecía en
S5 (la ayuda en tú, parte de la interfaz en vos) y se unificó. Los documentos
internos para desarrolladores (`claude/`, `.claude/`) no entran en esta regla.

## Estilo

- Go: `gofmt`, `go vet`, comentarios en español que expliquen el *por qué*.
- TS: componentes `PascalCase`, hooks `useX`, sin `any` salvo en bordes de JSON.
- Textos de UI en los dos idiomas (ver `claude/05-i18n.md`).
