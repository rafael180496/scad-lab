---
name: bambu-lab-printers
description: Perfiles de impresoras Bambu Lab (A1 mini, A1, P1P, P1S, P2S, X1C, X1E, H2D, H2S) y reglas de diseño FDM para que un .scad se imprima bien. Úsala al dimensionar un modelo, cuando el workspace tenga printer.json, al definir holguras/encajes/insertos/texto o al decidir orientación y voladizos.
---

# Impresoras Bambu Lab y diseño para FDM

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json` (ej. `{"printer":"bambu-a1-mini", ...}`),
   esa es la impresora. Copia su `id` a una constante en `[Hidden]`:
   `PRINTER = "bambu-a1-mini";`.
2. Si no hay `printer.json`, asume **`bambu-a1-mini`** (la placa más chica: lo que
   cabe ahí cabe en todas).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos listados en `verify` no están confirmados.

| id | Impresora | Volumen X×Y×Z (mm) | Boquilla | Multicolor | Cerrada | Notas |
|---|---|---|---|---|---|---|
| `bambu-a1-mini` | A1 mini | 180×180×180 | 0.4 | AMS lite, 4 | no | cama móvil; PLA/PETG/TPU |
| `bambu-a1` | A1 | 256×256×256 | 0.4 | AMS lite, 4 | no | cama móvil |
| `bambu-p1p` | P1P | 256×256×256 | 0.4 | AMS, 4 por unidad (hasta 16) | no | CoreXY abierta |
| `bambu-p1s` | P1S | 256×256×256 | 0.4 | AMS, hasta 16 | sí (pasiva) | ABS/ASA ok |
| `bambu-p2s` | P2S | 256×256×256 «verificar» | 0.4 | AMS / AMS 2 Pro «verificar» | sí | sucesora de P1 |
| `bambu-x1c` | X1 Carbon | 256×256×256 | 0.4 endurecida | AMS, hasta 16 | sí (pasiva) | lidar; fibras CF/GF |
| `bambu-x1e` | X1E | 256×256×256 | 0.4 endurecida | AMS, hasta 16 | sí, calefaccionada (~60 °C «verificar») | lidar |
| `bambu-h2d` | H2D | 325×320×325 una boquilla; 300×320×325 ambas; 350×320×325 total «verificar» | 2 × 0.4 | AMS 2 Pro/HT, hasta ~24 «verificar» | sí, calefaccionada (~65 °C) | doble boquilla |
| `bambu-h2s` | H2S | 340×320×340 «verificar» | 0.4 | AMS 2 Pro/HT «verificar» | sí, calefaccionada «verificar» | una boquilla |

- **Margen**: diseña dejando 2–5 mm libres respecto del volumen nominal (clips de
  placa, purga, borde/brim).
- **H2D**: si la pieza usa las **dos** boquillas (dos materiales o soporte
  soluble), usa el volumen "ambas boquillas"; con una sola, el de una boquilla.
- **Cama móvil (A1/A1 mini)**: piezas altas y finas oscilan. Orienta el lado
  largo en Y, agrega orejas (`mouse ears`) o base más ancha.
- **Multicolor**: el número de colores simultáneos lo fija el AMS (AMS lite = 4).
  Ver skill `openscad-3mf-multicolor`.

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

## 3. Reglas de diseño FDM (boquilla 0.4)

Detalle, tablas y snippets: [references/fdm-design-rules.md](references/fdm-design-rules.md).

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

## 4. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`).
- [ ] Apoya en z=0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0.9 mm (estructurales ≥ 1.6).
- [ ] Holguras parametrizadas según el tipo de ajuste.
- [ ] Agujeros horizontales grandes en lágrima.
- [ ] Texto ≥ 4–6 mm, trazo ≥ 0.8.
- [ ] Colores ≤ slots del AMS de la impresora.
