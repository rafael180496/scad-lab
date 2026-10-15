# Cómo crear una skill nueva para ScadLab

Esta carpeta es una **plantilla**: no es una skill operativa. Cópiala para
agregar conocimiento propio (una librería que uses, un tipo de pieza que
diseñes seguido, reglas de tu impresora o tu filamento).

## Pasos

1. **Copia la carpeta** `_skill-template/` dentro de `assets/skills/` (o de la
   carpeta de skills que use tu instalación de ScadLab).
2. **Renómbrala** en kebab-case, describiendo el tema: `openscad-threads`,
   `petg-settings`, `drawer-organizers`.
3. Abre `SKILL.md` y en el frontmatter:
   - `name`: **exactamente igual** al nombre de la carpeta. Solo minúsculas,
     números y guiones.
   - `description`: una o dos frases en español que digan **cuándo** usar la
     skill. Es lo que el agente lee para decidir si la carga: nombra los
     pedidos, palabras clave o errores que deberían dispararla. Mal: "Skill
     de roscas". Bien: "Diseño de roscas imprimibles en OpenSCAD. Úsala
     cuando el usuario pida tornillos, tuercas o tapas roscadas, o un render
     falle por un módulo de rosca desconocido."
4. Completa las secciones del cuerpo (Cuándo usar, Reglas, Flujo, Ejemplos,
   Errores comunes) y borra los comentarios de guía.
5. Mantén `SKILL.md` por debajo de ~250 líneas. El detalle va aparte:
   - `references/*.md`: tablas, explicaciones largas, especificaciones.
   - `references/*.json`: datos legibles por máquina.
   - `templates/*.scad`: código listo para copiar.
   Enlázalos desde `SKILL.md` con rutas relativas
   (`[tabla de holguras](references/clearances.md)`), así el agente solo los
   lee cuando los necesita.
6. Borra los `.gitkeep` de `references/` y `templates/` si agregas archivos, o
   las carpetas si no las usas.

## Reglas que toda skill debe respetar

Las skills se copian a cada workspace y el agente las combina; ninguna puede
contradecir el contrato de la app definido en `openscad-core`:

- Cada versión del modelo es un `.scad` nuevo `NNN-slug.scad`; nunca se
  sobrescribe ni se borra uno existente.
- Línea 1 de cada checkpoint:
  `// @checkpoint parent=<padre.scad> desc="<qué cambia>"`.
- Toda pieza de primer nivel dentro de `color()`.
- Parámetros arriba con comentarios de Customizer; unidades en milímetros.
- Validar el render y corregir errores en un checkpoint nuevo.
- Respetar `printer.json` si existe.

Toda plantilla `.scad` en `templates/` debe empezar con
`// @checkpoint parent= desc="..."` para poder copiarse directamente como
`001-<slug>.scad`.

## Idioma

Texto de la skill en español; identificadores, nombres de archivo y código
OpenSCAD en inglés.

## Cómo llega la skill al agente

ScadLab sincroniza las carpetas de skills a la ubicación que espera cada CLI
dentro del workspace:

| CLI | Carpeta |
|---|---|
| Claude Code | `.claude/skills/` |
| Codex / agentes compatibles | `.agents/skills/` |
| opencode | `.opencode/skills/` |
| Antigravity | `.agent/skills/` |

No hace falta copiarla a mano en cada una: basta con agregarla al origen y
dejar que la app sincronice. Las carpetas cuyo nombre empieza con `_` (como
esta plantilla) son material de apoyo, no skills para el agente.
