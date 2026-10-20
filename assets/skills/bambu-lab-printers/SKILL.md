---
name: bambu-lab-printers
description: Perfiles de impresoras Bambu Lab (A1 mini, A1, A2L, P1P, P1S, P2S, X1C, X1E, X2D, H2D, H2S, H2C) y reglas de diseño FDM para que un .scad se imprima bien, con multicolor AMS y exportación 3MF para Bambu Studio. Úsala cuando printer.json diga una impresora bambu-*, al dimensionar un modelo, al definir holguras/encajes/insertos/texto, al decidir orientación y voladizos o al preparar un archivo multicolor.
---

# Impresoras Bambu Lab y diseño para FDM

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json` (ej. `{"printer":"bambu-a1-mini", ...}`),
   esa es la impresora. Copia su `id` a una constante en `[Hidden]`:
   `PRINTER = "bambu-a1-mini";`.
2. Si no hay `printer.json`, asume **`bambu-a1-mini`** (la placa más chica: lo que
   cabe ahí cabe en todas).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos listados en `verify` no están confirmados;
   las URLs de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `bambu-a1-mini` | A1 mini | 180×180×180 | 4 / 16 | AMS lite (o hasta 4 AMS con el Hub de la serie A) | abierta | `multicolor_slots`, `filament_diameter_mm` |
| `bambu-a1` | A1 | 256×256×256 | 4 / 16 | AMS lite (o hasta 4 AMS con el Hub de la serie A) | abierta | `filament_diameter_mm` |
| `bambu-a2l` | A2L | 330×320×325 | 4 / 19 | AMS lite / AMS (hasta 4 AMS + 1 lite) | abierta | `default_colors`, `filament_diameter_mm` |
| `bambu-p1p` | P1P | 256×256×256 | 4 / 16 | AMS (hasta 4 unidades = 16) | abierta | `filament_diameter_mm` |
| `bambu-p1s` | P1S | 256×256×256 | 4 / 16 | AMS (hasta 4 unidades = 16) | cerrada | `filament_diameter_mm` |
| `bambu-p2s` | P2S | 256×256×256 | 4 / 20 | AMS 2 Pro / AMS HT (hasta 20 ranuras) | cerrada | — |
| `bambu-x1c` | X1C | 256×256×256 | 4 / 16 | AMS (hasta 4 unidades = 16) | cerrada | `max_nozzle_temp_c`, `max_bed_temp_c`, `filament_diameter_mm`, `heated_chamber`, `enclosed` |
| `bambu-x1e` | X1E | 256×256×256 | 4 / 16 | AMS (hasta 4 unidades = 16) | cerrada, calefaccionada | `max_nozzle_temp_c`, `max_bed_temp_c`, `filament_diameter_mm`, `heated_chamber` |
| `bambu-x2d` | X2D | 256×256×260 | 4 / 25 | AMS 2 Pro / AMS HT (hasta 25 colores con bobina externa) | cerrada, calefaccionada | — |
| `bambu-h2d` | H2D | 325×320×325 | 4 / 25 | AMS 2 Pro / AMS HT (hasta 25 colores con bobina externa) | cerrada, calefaccionada | — |
| `bambu-h2s` | H2S | 340×320×340 | 4 / 24 | AMS 2 Pro / AMS HT (hasta 24 colores) | cerrada, calefaccionada | — |
| `bambu-h2c` | H2C | 305×320×320 | 4 / 25 | AMS 2 Pro / AMS HT + Vortek (6 hotends intercambiables + 1 fija) | cerrada, calefaccionada | `multicolor_slots` |

- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json` (lo que el usuario eligió para este workspace; nace en 4), no el
  tope de la máquina (`max_colors`). El usuario lo cambia en «Configuración del
  workspace» o tú con `set_workspace_printer`.
- **Margen**: diseña dejando 2–5 mm libres respecto del volumen nominal (clips de
  placa, purga, borde/brim).
- **Doble boquilla (H2D, X2D, H2C)**: `build_volume` es el de **una** boquilla; si la
  pieza usa las dos (dos materiales o soporte soluble), usa el de ambas
  (`build_volume_dual_nozzle`: H2D 300×320×325; X2D 235,5×256×256; H2C 300×320×325).
  La H2C añade el sistema **Vortek** (cambio de hotend sin contacto): con menos de
  7 filamentos casi no hay purga.
- **Cama móvil (A1/A1 mini)**: piezas altas y finas oscilan. Orienta el lado
  largo en Y, agrega orejas (`mouse ears`) o base más ancha.
- **Marco abierto (A1, A1 mini, A2L, P1P)**: ABS/ASA/PC/PA solo en piezas pequeñas.
  Cerradas (P1S, P2S, X1C, X1E, X2D, H2*): ABS/ASA sin problema; la X2D y la serie
  H tienen además cámara calefaccionada (hasta 65 °C).
- **Descontinuadas**: P1P, X1C y X1E ya no están en la tienda oficial (la X2D
  reemplaza a la X1C); sus temperaturas máximas no están confirmadas.
- **Multicolor (AMS)**: el número de colores simultáneos lo fija el AMS (AMS lite = 4
  por unidad; ver la columna Sistema). Ver skill `openscad-3mf-multicolor`.

## 2. Validar que cabe en la placa

Pega el snippet [references/plate_check.scad](references/plate_check.scad) al
final del checkpoint y llámalo con el tamaño total ya orientado:

```openscad
/* [Hidden] */
PRINTER = "bambu-a1-mini";
...
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Si no cabe: reduce, reorienta (diagonal no se considera), o parte la pieza con
uniones (cola de milano, pasadores Ø de 4–5 mm con 0.2 de holgura, tornillos).

Con las herramientas de ScadLab (MCP): `measure` dice si cabe (`fitsBuildVolume`,
`overflowMm`) y `suggest_orientation` prueba las 6 caras hacia abajo (tamaño,
si cabe, área en voladizo, contacto con la placa) con las `operations` listas
para `edit_mesh`; si nada cabe, da la escala máxima y por dónde partir. Para el
peso con otro material o relleno, `estimate_print` (materiales en
`list_materials`).

## 3. Reglas de diseño FDM (boquilla 0.4)

Detalle, tablas y snippets (lágrima, chaflán, insertos, texto):
`openscad-core/references/fdm-design-rules.md`.

| Tema | Regla |
|---|---|
| Voladizos | ≤ 45° desde la vertical sin soportes. Más: chaflán, cambiar orientación o soporte. |
| Puentes | Horizontales entre dos apoyos: hasta ~20 mm cómodo en PLA; más largo, soportes o arco. |
| Ancho de línea | ~0.42 mm (perfil Bambu con 0.4). |
| Pared mínima | 2 perímetros ≈ 0.9 mm. Estructural: 1.6–2.4 mm. Paredes en múltiplos de 0.42–0.45. |
| Detalle mínimo | 0.4–0.5 mm en XY; en Z, una altura de capa. |
| Pata de elefante | Chaflán de 0.4–0.6 mm a 45° en el borde inferior que apoya en la placa. |
| Agujeros verticales | Salen ~0.1–0.2 mm más chicos: sumar 0.2 al Ø nominal (ej. M3 pasante Ø 3.4). |
| Agujeros horizontales | Forma de **lágrima** (punta arriba a 45°) para Ø > 5 mm, o puente de sacrificio. |
| Base | La cara más grande y plana sobre la placa (z=0). Sin esquinas vivas largas que se levanten (warping): radio ≥ 2 mm. |

### Holguras (por lado, entre piezas impresas)

| Ajuste | Holgura |
|---|---|
| A presión / interferencia leve | 0.1–0.2 mm |
| Deslizante (tapa corrediza, eje que gira) | 0.3–0.4 mm |
| Impresas juntas (print-in-place: bisagras, cadenas) | 0.3–0.5 mm (≥ 0.4 recomendable en A1 mini) |
| Pieza impresa sobre pieza comprada (rodamiento, imán) | 0.1–0.15 sobre el Ø, probar con un test |

Declara holguras como parámetro (`clearance = 0.2; // [0:0.05:0.8]`): cada
impresora/filamento ajusta distinto.

### Orientación

- La resistencia es menor **entre capas** (eje Z). Ganchos, soportes en L y clips
  se orientan para que la carga sea paralela a las capas, no las separe.
- Agujeros que necesitan precisión: verticales (eje Z).
- Superficies visibles arriba (mejor acabado) o contra la placa texturada/lisa.
- Si una orientación necesita soportes y otra no, gana la que no.

### Roscas

- M6 o menor: **no** imprimir la rosca. Usar insertos térmicos, tuerca embutida
  (alojamiento hexagonal) o roscar directo un tornillo autorroscante.
- M8+ se puede imprimir (capas 0.12–0.16, holgura 0.2–0.3). Usar
  `threaded_rod()` de BOSL2 (skill `openscad-libraries`).

### Insertos térmicos M3 (heat-set)

| Inserto | Ø agujero | Profundidad | Ø exterior del boss |
|---|---|---|---|
| M3 × 5.7 (ruthex / tipo estándar) | 4.0 «verificar ficha de la marca» | ≥ 6.7 (largo + 1) | ≥ 7.5–8 |
| M3 × 4 corto (CNC Kitchen corto) | 4.0–4.2 «verificar» | ≥ 5 | ≥ 7.5 |
| M3 × 3 | 4.0 «verificar» | ≥ 4 | ≥ 7 |

Agujero vertical (eje Z), con chaflán de entrada de 0.5 mm. El boss lleva al
menos 3–4 perímetros. **Siempre** confirmar con la ficha del fabricante del
inserto: cada marca usa Ø distintos.

### Texto

| Tipo | Mínimo |
|---|---|
| Altura de letra (`size`) | 6 mm cómodo; 4 mm mínimo, fuente bold |
| Trazo de la letra | ≥ 0.8 mm (2 líneas) |
| Relieve | 0.4–1 mm (o 1–2 capas para cambio de color por capa) |
| Grabado | 0.4–0.6 mm de profundidad |
| Texto en cara vertical | más legible que en techo; evitar texto en voladizo |

Fuente recomendada: `"Liberation Sans:style=Bold"`.

### Altura de capa (perfiles Bambu Studio con 0.4)

| Capa | Perfil | Uso |
|---|---|---|
| 0.08 | Extra Fine | miniaturas, texto chico, roscas finas |
| 0.12 | Fine | detalle, curvas suaves, engranajes |
| 0.16 | Optimal | buen compromiso calidad/tiempo |
| 0.20 | Standard | uso general (por defecto) |
| 0.28 | Extra Draft | piezas grandes funcionales, prototipos |

Si el modelo tiene alturas críticas (relieves de color, encajes en Z), hazlas
**múltiplo de la capa** prevista (ej. relieve de 0.6 con capa 0.2).

## 4. Qué formato exportar

La herramienta MCP `export` (y los botones de la app) escribe en `exports/`:

| Para | Formato | Nota |
|---|---|---|
| **Bambu Studio** (recomendado) | **`3mf`** | Un objeto con una parte por color; cada parte con su extrusor (`Metadata/model_settings.config`). Los colores de los filamentos los pone el AMS del usuario. |
| OrcaSlicer | `3mf` | Igual. |
| Cualquiera | `stl-per-color` | Un STL por color en un zip. En Bambu Studio, importa los STL juntos y responde **Sí** a «¿cargar como un solo objeto con varias partes?»; después asigna el filamento de cada parte. |
| Una sola pieza | `stl` | Sin color. |

No uses `3mf-prusa`: es para PrusaSlicer. Detalle del formato y qué lee cada slicer:
skill `openscad-3mf-multicolor`.

## 5. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`).
- [ ] Apoya en z=0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0.9 mm (estructurales ≥ 1.6).
- [ ] Holguras parametrizadas según el tipo de ajuste.
- [ ] Agujeros horizontales grandes en lágrima.
- [ ] Texto ≥ 4–6 mm, trazo ≥ 0.8.
- [ ] Colores ≤ `colors` del workspace (`printer.json`), un `color()` por filamento.
- [ ] Exportación `3mf` si el slicer es Bambu Studio (ver sección 5).
