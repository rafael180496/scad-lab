# Arquitectura

```
React (frontend/src)  ──bindings Wails──▶  App (app*.go)  ──▶  backend/*
       ▲                                        │
       └──────── eventos Wails (S1+) ◀──────────┘
```

- **`App` es el único objeto enlazado.** Sus métodos públicos son la API del
  frontend; se reparten por área en `app_<área>.go`. Un binding valida la
  entrada, delega en `backend/*` y devuelve structs con tags `json` en
  camelCase. Toda lógica vive en `backend/`, no en `app*.go`.
- **`backend/` no conoce a Wails** (salvo diálogos en `app_settings.go`): así se
  testea con `go test` sin ventana.
- **El frontend no llama a Go fuera de `wailsjs/`.** Los bindings se regeneran
  con `wails generate module`.

## Workspace y checkpoints

Un workspace es una carpeta del usuario (`~/Documents/ScadLab/<slug>` por
defecto). Los `.scad` son la **fuente de verdad**; `scadlab.db` solo indexa:

```
disco: NNN-slug.scad  ──Scan()──▶  CheckpointFile{file, parent, desc}
                                   └─SyncCheckpoints()──▶ tabla checkpoints
                                                          (+ label, collapsed, session: solo UI)
```

- Inmutables: ScadLab y los agentes crean archivos nuevos, nunca reescriben.
- El árbol sale de `parent=` del encabezado. Un padre inexistente deja el
  nodo en la raíz marcado como huérfano, no lo esconde.
- Perder la base no pierde nada: el rescan reconstruye el índice.

## Varios proyectos abiertos (S8)

Una ventana, hasta 4 workspaces abiertos como pestañas (`app_projects.go`). Lo abierto y
el activo se persisten en settings; los de fondo siguen vivos (un watcher por proyecto,
auto-render, terminales de agente y auto-iteración), y el límite de 2 terminales de
agente es por proyecto. Cada agente lanzado desde ScadLab recibe las carpetas de los
otros proyectos abiertos (`--add-dir`) para leer y reutilizar piezas; escribir, solo en
el suyo. Detalle y contrato: [09-agents-ai.md](09-agents-ai.md).

## Skills

```
assets/skills/ (embed) ─┐
<datos>/skills/ (user) ─┴─ Discover() ─ registro `skills` (enabled) ─ Sync() ─▶
     <ws>/.claude/skills  <ws>/.agents/skills  <ws>/.opencode/skills  <ws>/.agent/skills
```

Una skill de usuario con el mismo nombre que una incluida la reemplaza. `Sync`
marca cada copia con `.scadlab-managed` y solo toca esas: una skill que el
usuario dejó a mano en el workspace no se pisa.

## Datos por SO

| SO | Datos (`scadlab.db`, `skills/`, `cache/`) |
|---|---|
| macOS | `~/Library/Application Support/ScadLab/` |
| Windows | `%AppData%\ScadLab\` |
