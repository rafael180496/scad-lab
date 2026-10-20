---
name: openscad-templates
description: Biblioteca de plantillas OpenSCAD paramétricas listas para imprimir (caja con tapa, gabinete para PCB, escuadra, clip de cable, perilla, engranaje, bisagra print-in-place, soporte de teléfono, placa bicolor, contenedor Gridfinity). Úsala al empezar un proyecto nuevo o cuando el pedido se parezca a alguna de estas piezas, antes de modelar desde cero.
---

# Plantillas paramétricas

Todas las plantillas de [templates/](templates/) cumplen el contrato de
`openscad-core`: línea `@checkpoint`, parámetros de Customizer arriba, toda pieza
de primer nivel en `color()`, `eps` en los cortes, `$fa/$fs` con `$preview`,
apoyo en z=0, `assert` de parámetros y de tamaño de placa, y reglas FDM
(`openscad-core/references/fdm-design-rules.md` y la skill de marca de la impresora).
Por defecto caben en una placa de **180×180×180** (A1 mini, Prusa MINI+); `create_from_template`
los ajusta a la impresora del workspace.

## Cómo usarlas

Con las herramientas de ScadLab (MCP): `list_templates` y luego
`create_from_template(name, parameters)`, que crea el checkpoint ya con los
parámetros cambiados, `PLATE` ajustado a la impresora del workspace y la línea
`@checkpoint` escrita (raíz en un workspace vacío; si no, hijo del padre
activo). Los valores salen de `get_parameters` y se cambian después con
`set_parameters`. A mano, los pasos son estos:

1. Elige la plantilla más cercana al pedido (tabla de abajo).
2. **Cópiala** al workspace como primer checkpoint: `001-<slug>.scad`
   (ej. `001-sensor-enclosure.scad`). Si el workspace ya tiene checkpoints,
   usa el número siguiente y pon como `parent` el checkpoint del que parte.
3. Actualiza la línea 1: `// @checkpoint parent= desc="<qué es este modelo>"`.
4. Ajusta **solo los parámetros** para el primer checkpoint; los cambios de
   geometría van en checkpoints siguientes.
5. Si hay `printer.json`, cambia `PLATE` en `[Hidden]` al volumen de esa
   impresora (`references/printers.json` de la skill de marca de esa impresora).
6. Renderiza y revisa warnings (ver `openscad-core`, sección 7).

Nunca edites los archivos de `templates/` desde un workspace: son la fuente.

## Catálogo

| Plantilla | Parámetros principales | Cuándo usarla |
|---|---|---|
| [`box-with-lid.scad`](templates/box-with-lid.scad) | `inner_w/d/h`, `wall`, `floor_t`, `corner_r`, `lid_type` (`snap`/`slide`), `clearance`, `lip_h`, `PART` | Cajas, estuches, organizadores con tapa. `snap` = labio a presión; `slide` = tapa corrediza por ranuras. |
| [`electronics-enclosure.scad`](templates/electronics-enclosure.scad) | `pcb_w/d/t`, `hole_inset`, `component_h`, `standoff_h/d`, `insert_d`, `insert_depth`, `usb_side/w/h/offset/center_z`, `vents`, `PART` | Gabinete para Arduino/ESP32/Raspberry Pico u otra PCB con 4 agujeros en las esquinas; standoffs para insertos térmicos M3. |
| [`wall-bracket.scad`](templates/wall-bracket.scad) | `arm_len`, `plate_h`, `width`, `thickness`, `gusset`, `screw_d`, `head_d`, `countersink`, `wall_holes`, `arm_holes` | Escuadras de estante, soportes en L. Se imprime acostada; agujeros en lágrima. |
| [`cable-clip.scad`](templates/cable-clip.scad) | `cable_d`, `cable_count`, `opening_ratio`, `ring_t`, `width`, `mount` (`screw`/`adhesive`) | Clips para fijar uno o varios cables a una pared o escritorio. |
| [`knob.scad`](templates/knob.scad) | `knob_d/h`, `flutes`, `shaft_d`, `shaft_flat`, `shaft_clear`, `shaft_depth`, `set_screw`, `pointer_depth` | Perillas para potenciómetros/encoders con eje D de 6 mm (u otro, cambiando `shaft_d`/`shaft_flat`). Se imprime boca abajo. |
| [`spur-gear.scad`](templates/spur-gear.scad) | `gear_module`, `teeth`, `thickness`, `pressure_angle`, `backlash`, `bore_d`, `hub_d/h`, `show_mate`, `mate_teeth` | Engranajes rectos con involuta propia (sin librerías). Distancia entre centros = `m*(z1+z2)/2`. |
| [`hinge-print-in-place.scad`](templates/hinge-print-in-place.scad) | `hinge_len`, `leaf_w/t`, `knuckles` (impar), `knuckle_d`, `clearance`, `holes_per_leaf` | Bisagras que salen armadas de la impresora (conos de 45°, sin soportes). |
| [`phone-stand.scad`](templates/phone-stand.scad) | `phone_t`, `angle`, `width`, `thickness`, `lip_h`, `back_len`, `base_depth`, `cable_slot` | Soportes de escritorio para teléfono/tablet chica, con ranura para el cable. |
| [`nameplate-two-color.scad`](templates/nameplate-two-color.scad) | `label`, `font`, `text_size`, `text_h`, `mode` (`raised`/`inlay`), `plate_w/d/h`, `border_w`, `hole_d`, `base_color`, `text_color`, `PART` | Carteles, etiquetas y llaveros bicolor (AMS, MMU, CFS, ACE…; ver `openscad-3mf-multicolor`). |
| [`gridfinity-bin.scad`](templates/gridfinity-bin.scad) | `grid_x/y`, `height_units`, `wall`, `floor_t`, `dividers_x/y`, `stacking_lip`, `magnets` | Contenedores Gridfinity (celda 42 mm, unidad de altura 7 mm). A1 mini: hasta 4×4. |

## Notas por plantilla

- **box-with-lid**: holgura `snap` 0.1–0.2, `slide` 0.3–0.4. La tapa
  deslizante entra por el extremo +X; necesita `wall >= 2`.
- **electronics-enclosure**: los agujeros de la PCB se asumen en las 4 esquinas
  a `hole_inset`. Para otro patrón, en un checkpoint siguiente reemplaza
  `pcb_holes` (en `[Hidden]`) por la lista real de coordenadas desde la esquina
  de la PCB. Verifica `insert_d` con la ficha del inserto.
- **wall-bracket**: el refuerzo (`gusset`) va en la esquina interior; los
  agujeros se reparten fuera de su zona y se avellanan del lado interno.
- **cable-clip**: `opening_ratio < 1` es lo que retiene el cable; bajar a 0.6
  si se suelta, subir a 0.8 si cuesta meterlo.
- **knob**: la marca indicadora queda alineada con el plano del eje D.
- **spur-gear**: con menos de ~17 dientes (20°) el diente real tendría
  socavado; esta plantilla no lo modela y el engrane puede trabar. Para
  helicoidales, cremalleras o cónicos usa BOSL2 (`openscad-libraries`).
- **hinge-print-in-place**: imprimir con capa 0.2 o menor; mover la bisagra con
  firmeza la primera vez para despegar la primera capa entre piezas. Ambas hojas
  tienen el mismo color por defecto: dos colores obligan a cambiar filamento en
  cada capa.
- **phone-stand**: el `assert` impide una base más corta que la proyección del
  respaldo (volcaría).
- **nameplate-two-color**: OpenSCAD no mide el ancho del texto; si el texto
  se sale del borde, baja `text_size` o sube `plate_w`. `text_h` en múltiplos de
  la altura de capa.
- **gridfinity-bin**: el perfil del labio de apilado sigue la especificación
  conocida (0.7 + 1.8 + 1.9) «verificar» contra la base/placa que use el
  usuario si mezcla generadores.

## Exportar por color

Las plantillas con `PART` exportan cada pieza o color por separado:

```bash
openscad -D 'PART="base"' -o /tmp/base.stl 001-nameplate.scad
openscad -D 'PART="text"' -o /tmp/text.stl 001-nameplate.scad
```
