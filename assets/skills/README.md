# Skills de ScadLab

Paquete de skills (formato Claude Code Agent Skills) que ScadLab copia dentro de
cada *workspace* de modelado. Le enseñan al agente IA que trabaja ahí (Claude
Code, opencode, Codex, Antigravity, Ollama) a diseñar archivos `.scad`
correctos, paramétricos e imprimibles en impresoras Bambu Lab.

## Skills incluidas

| Skill | Propósito |
|---|---|
| [`openscad-core`](openscad-core/SKILL.md) | Reglas de modelado paramétrico, convención de checkpoints, CSG limpio, rendimiento y lectura de errores. **Base de todas las demás.** |
| [`bambu-lab-printers`](bambu-lab-printers/SKILL.md) | Perfiles de impresoras Bambu Lab (volumen, boquilla, AMS) y reglas de diseño FDM: voladizos, holguras, insertos, texto mínimo. |
| [`openscad-3mf-multicolor`](openscad-3mf-multicolor/SKILL.md) | Modelos multicolor para AMS: una pieza por `color()`, export 3MF y formato del archivo 3MF. |
| [`openscad-templates`](openscad-templates/SKILL.md) | Biblioteca de plantillas paramétricas listas para arrancar un proyecto (cajas, gabinetes, engranajes, Gridfinity…). |
| [`openscad-libraries`](openscad-libraries/SKILL.md) | Cuándo y cómo usar BOSL2, MCAD y NopSCADlib desde la carpeta `lib/` del workspace. |
| [`openscad-stl-editing`](openscad-stl-editing/SKILL.md) | Modificar un STL (o 3MF/OBJ) existente sin tocarlo: `import()` en un checkpoint nuevo y recetas de agujeros, avellanados, cortes con un plano, refuerzos, texto, escala, partir para la placa y espejar; límites de las mallas. |
| [`_skill-template`](_skill-template/README.md) | Plantilla para que el usuario cree skills nuevas. No es una skill operativa. |

## Contrato con la app (resumen)

Todas las skills respetan estas reglas; el detalle está en `openscad-core`:

1. **Checkpoints inmutables**: cada versión es un `.scad` nuevo `NNN-slug.scad`;
   nunca se sobrescribe ni borra uno existente.
2. **Primera línea** de cada checkpoint:
   `// @checkpoint parent=<archivo-padre.scad> desc="<qué cambia>"`.
3. **Color obligatorio**: toda pieza de primer nivel dentro de `color(...)`.
4. **Parámetros arriba**, con comentarios de Customizer.
5. **Validar** cada checkpoint (herramientas MCP `render`/`preview` del servidor `scadlab`, o `openscad -o` si no está) y
   corregir lo que reporte `RENDER_ERRORS.md` en un checkpoint **nuevo**.
6. **Milímetros** siempre. Respetar `printer.json` si existe.

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
- Archivos legibles por la app: `bambu-lab-printers/references/printers.json`.
