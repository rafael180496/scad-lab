# Skills de ScadLab

Paquete de skills (formato Claude Code Agent Skills) que ScadLab copia dentro de
cada *workspace* de modelado. Le enseñan al agente IA que trabaja ahí (Claude
Code, opencode, Codex, Antigravity, Ollama) a diseñar archivos `.scad`
correctos, paramétricos e imprimibles en la impresora del workspace: Bambu Lab, Prusa,
Creality, Elegoo, Anycubic, Snapmaker o UltiMaker.

## Skills incluidas

| Skill | Propósito |
|---|---|
| [`openscad-core`](openscad-core/SKILL.md) | Reglas de modelado paramétrico, convención de checkpoints, CSG limpio, rendimiento y lectura de errores. **Base de todas las demás.** |
| [`bambu-lab-printers`](bambu-lab-printers/SKILL.md) | **Skill de marca.** Bambu Lab (A1 mini, A1, A2L, P1P, P1S, P2S, X1C, X1E, X2D, H2D, H2S, H2C): perfiles, AMS, formato a exportar y reglas de diseño FDM (voladizos, holguras, insertos, texto mínimo). |
| [`prusa-printers`](prusa-printers/SKILL.md) | **Skill de marca.** Prusa (MK4S, CORE One+, CORE One L+, MINI+, XL): perfiles, MMU3 y toolchanger, exportar para PrusaSlicer (`3mf-prusa`). |
| [`creality-printers`](creality-printers/SKILL.md) | **Skill de marca.** Creality (Ender-3 V3, K1, K2, Hi): perfiles, CFS y reglas propias. |
| [`elegoo-printers`](elegoo-printers/SKILL.md) | **Skill de marca.** Elegoo (Neptune 4, Centauri Carbon): perfiles y CANVAS. |
| [`anycubic-printers`](anycubic-printers/SKILL.md) | **Skill de marca.** Anycubic (Kobra 2, 3, S1, X): perfiles, ACE y el orden de las medidas de la Kobra 2. |
| [`snapmaker-printers`](snapmaker-printers/SKILL.md) | **Skill de marca.** Snapmaker (U1, J1s, Artisan): toolchanger, IDEX, doble extrusión y volumen según el modo. |
| [`ultimaker-printers`](ultimaker-printers/SKILL.md) | **Skill de marca.** UltiMaker (S3, S5, S7, S8, Factor 4): filamento de 2,85 mm, doble extrusión y soporte soluble. |
| [`openscad-3mf-multicolor`](openscad-3mf-multicolor/SKILL.md) | Modelos multicolor para cualquier sistema (AMS, MMU3, CFS, ACE, toolchanger, IDEX, doble extrusión): una pieza por `color()`, qué formato exportar según el slicer y el formato interno de los 3MF. |
| [`openscad-templates`](openscad-templates/SKILL.md) | Biblioteca de plantillas paramétricas listas para arrancar un proyecto (cajas, gabinetes, engranajes, Gridfinity…). |
| [`openscad-libraries`](openscad-libraries/SKILL.md) | Cuándo y cómo usar BOSL2, MCAD y NopSCADlib desde la carpeta `lib/` del workspace. |
| [`openscad-stl-editing`](openscad-stl-editing/SKILL.md) | Modificar un STL (o 3MF/OBJ) existente sin tocarlo: `import()` en un checkpoint y recetas de agujeros, avellanados, cortes con un plano, refuerzos, texto, escala, partir para la placa y espejar; límites de las mallas. |
| [`openscad-from-image`](openscad-from-image/SKILL.md) | Modelar a partir de una imagen que el usuario adjunta (`refs/images/`): foto de una pieza, logo o placa plana, relieve o litofanía, boceto acotado o varias vistas. Escala (una medida o un objeto conocido, con la fuente de cada cifra en `references/standard-sizes.md`), flujo crear → render → `compare_with_image` → corregir y los límites de lo que una foto permite. Herramientas: `image_info`, `compare_with_image`, `trace_image_outline`, `image_to_heightmap`. |
| [`_skill-template`](_skill-template/README.md) | Plantilla para que el usuario cree skills nuevas. No es una skill operativa. |

## Contrato con la app (resumen)

Todas las skills respetan estas reglas; el detalle está en `openscad-core`:

1. **Editar el archivo que el usuario nombra**: si nombra un archivo existente o se
   refiere al abierto/seleccionado y pide cambios, se edita con `update_checkpoint`
   (guarda antes la versión anterior en el historial). Un `.scad` nuevo
   `NNN-slug.scad` solo si pide algo nuevo, una variante u «otra versión». Los `.scad`
   se leen con `read_checkpoint` y nunca se escriben con las herramientas de
   archivo/shell del CLI; no se renombran ni se borran.
2. **Primera línea** de cada checkpoint:
   `// @checkpoint parent=<archivo-padre.scad> desc="<qué cambia>"`.
3. **Color obligatorio**: toda pieza de primer nivel dentro de `color(...)`.
4. **Parámetros arriba**, con comentarios de Customizer.
5. **Validar** cada checkpoint (herramientas MCP `render`/`preview` del servidor `scadlab`, o `openscad -o` si no está) y
   corregir lo que reporte `RENDER_ERRORS.md` con `update_checkpoint` sobre el mismo archivo.
6. **Milímetros** siempre. Respetar `printer.json` si existe: su `colors` es el máximo de
   colores del workspace.

## Skills de marca y skills de cada workspace

Las siete `*-printers` son **skills de marca**: cada una documenta una marca y trae el catálogo
de sus impresoras (`references/printers.json`, esquema v2 con `sources` y `verify`) y su
`references/plate_check.scad`. Un workspace no recibe todas: lleva las skills de la lista
propia del workspace (Configuración del workspace), que por defecto son las encendidas en
Configuración → Skills que no son de marca **más la de la marca de su impresora**.
`printers.BrandSkills()` dice cuáles son de marca y `set_workspace_printer` (MCP) cambia la
de un workspace al cambiar de marca. Las skills del usuario no cambian el catálogo.
Cómo agregar una marca o un modelo: [claude/08-openscad.md](../../claude/08-openscad.md#impresoras-s14-siete-marcas).

## Convención de carpetas

```
assets/skills/
├── README.md                    este archivo
└── <nombre-skill>/              kebab-case, igual al campo `name`
    ├── SKILL.md                 frontmatter YAML (name, description) + cuerpo
    ├── references/*.md|*.json   detalle extenso, cargado bajo demanda
    └── templates/*.scad         código listo para copiar
```

- Cada `SKILL.md` se mantiene por debajo de ~250 líneas; lo extenso vive en
  `references/` o `templates/` y se enlaza con ruta relativa.
- La `description` dice **cuándo** usar la skill: es lo que dispara su carga.
- Texto en español; identificadores y código OpenSCAD en inglés.
- La app sincroniza estas carpetas a la ubicación de cada CLI
  (`.claude/skills`, `.agents/skills`, `.opencode/skills`, `.agent/skills`).
- Archivos legibles por la app: `<marca>-printers/references/printers.json` (el catálogo de
  impresoras, uno por marca). Tabla de cada `SKILL.md` y `plate_check.scad` son espejo del
  JSON y los tests (`backend/printers`) lo comprueban.
- Reglas FDM genéricas y snippets: `openscad-core/references/fdm-design-rules.md`.
