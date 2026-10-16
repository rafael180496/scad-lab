# S7 — Medición e impresión (piezas, peso, costo, placa)

Fase del [roadmap](../../claude/01-roadmap.md). Incluye 9.5 (panel de costo)
que había quedado pendiente de S9.

## Decisiones (architect)

1. **Piezas del modelo = partes por color** (reutiliza `backend/threemf`
   `Split` y la preparación de `backend/exporter`): un render a 3MF con color
   se reparte en una malla por color. El visor carga **cada parte por
   separado, con su color real** (hasta ahora pintaba todo con una muestra:
   pendiente de S2) y cada parte es **seleccionable con un clic**.
2. **Estimación en Go, única** (`backend/estimate`), usada por el visor, el
   panel de costo y la herramienta MCP `measure` (que hoy da peso de sólido al
   100 %: se corrige). Modelo FDM simple y explicado al usuario:
   - `shell = min(volumen, área × espesorDePared)` con
     `espesorDePared = perímetros × anchoDeLínea` (default 2 × 0,42 mm);
   - `interior = volumen − shell`;
   - `efectivo = shell + interior × relleno%`;
   - `masa_g = efectivo_mm3 / 1000 × densidad_g_cm3`;
   - `largo_m = masa / (densidad × π × (diámetro/2)²)` (diámetro 1,75 mm);
   - `costo = masa / 1000 × costo_por_kg`.
   Siempre rotulado como **estimación** (sin soportes, sin purga del AMS, sin
   balsa); el número exacto lo da el laminador.
3. **Placa**: bbox global vs volumen de la impresora del workspace; aviso en el
   visor (contorno de la cama en rojo + texto) y en la barra de estado.
   Ya existe el cálculo en `backend/mcp` (`measure`): mover a `estimate`/un
   helper común, sin duplicar.

## Reparto (dos devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance |
|---|---|---|
| 7.A partes + estimación (Go) | openscad-dev | `backend/estimate/**` (nuevo), `backend/parts/**` (nuevo), `backend/meshserve/**` (ruta nueva de partes), `app_parts.go` (nuevo), `backend/mcp/**` (solo `measure` usa `estimate` y agrega partes), `claude/14-medicion.md` (nuevo) + fila en `CLAUDE.md` |
| 7.B visor por partes + panel Impresión (UI) | fullstack-dev | `frontend/**`, `backend/store/settings.go` (settings nuevos), `index.html`, `CHANGELOG.md`, `scripts/help-shots.sh`, `docs/screenshots/**`, `claude/06-*.md`, `claude/12-paneles.md` (panel nuevo), `claude/01-roadmap.md` |

### Contrato 7.A → 7.B

- Caché de partes por clave de render: `<cacheDir>/<key>.parts/NN.stl` (STL
  binario por parte) + `<key>.parts/manifest.json`. Se generan una vez por
  clave (render 3MF con color + `Split`); si el modelo no tiene `color()`, una
  sola parte = la malla del render normal.
- HTTP (middleware, igual que `/mesh/`): `GET /parts/<64 hex>/<NN>.stl`, con
  la misma validación estricta (regex de clave + `^[0-9]{2}\.stl$`; 404 para
  todo lo demás).
- Binding `GetModelParts(workspaceId int64, fileName string) (parts.Model, error)`:
  ```
  Model { key, parts: [ { index, name, color "#RRGGBB", url "/parts/<key>/NN.stl",
          info mesh.Info } ], total mesh.Info (bbox global), plate {x,y,z},
          fits bool, overflow {x,y,z} (mm que sobran por eje, 0 si cabe) }
  ```
  Requiere un render OK del checkpoint (si no, error claro). Corre el render 3MF
  en el motor existente (cola/cancelación por workspace).
- Binding `EstimatePrint(workspaceId int64, fileName string, opts estimate.Options) (estimate.Result, error)`:
  ```
  Options { infillPercent, wallLoops, lineWidthMm, filamentDiameterMm,
            defaultMaterialId, partMaterials map[int]string /* índice→material */ }
  Result  { parts: [ { index, materialId, volumeMm3, effectiveMm3, massG,
            lengthM, cost } ], total { massG, lengthM, cost }, currency,
            assumptions: [string] /* textos i18n */ }
  ```
  Con `opts` vacíos usa los settings. Rápido (sin render: usa el manifest de
  partes en caché).
- Settings nuevos (7.B los agrega en `store.Settings`, 7.A los lee con defaults
  si no están): `wallLoops` (2), `lineWidthMm` (0.42), `filamentDiameterMm`
  (1.75), `currency` ("USD"; símbolo según `Intl`). `infillPercent` y
  `defaultMaterial` ya existen; materiales en la tabla `materials`.

### 7.B — UI

- Visor: carga las partes (`GetModelParts` + `fetch` de cada URL, en la caché
  LRU por clave), **colores del modelo** por defecto con interruptor «Colores
  del modelo / Muestra» (las 6 muestras siguen); **clic en una parte** la
  resalta (contorno/emisivo, el resto atenuado) y abre una tarjeta con: color,
  tamaño X×Y×Z, volumen, peso, largo de filamento y costo estimados, material
  (selector por parte), triángulos y si es cerrada; Esc o clic en el vacío
  deselecciona. La vista dividida conserva la selección.
- **Placa**: si no cabe, contorno de la cama en rojo, aviso con cuántos mm
  sobran por eje y sugerencia (rotar, partir, otra impresora); también en la
  barra de estado.
- **Panel nuevo «Impresión»** (acoplable, como los demás; id `print`): tabla
  por parte (color, material, peso, metros, costo) + total; controles de
  relleno, perímetros, ancho de línea, diámetro de filamento y moneda (cambian
  en vivo con `EstimatePrint`, debounce); nota de supuestos; botón «Exportar
  3MF» (reusa S9). Atajo en el menú Ver como el resto.
- Capturas (banco): `viewer-parts` (modelo bicolor con colores reales y una
  parte seleccionada con su tarjeta), `print-panel`, `plate-overflow` (pieza
  que no cabe en una A1 mini). Ayuda: temas `visor-medir` e `imprimir-costo`
  (quitar «En desarrollo»), con la fórmula explicada en simple.

## Pruebas

- 7.A: `estimate` con casos de mano (cubo 20 mm: volumen 8000, área 2400,
  pared 0,84 → shell 2016; con 15 %: efectivo = 2016 + 5984×0,15 = 2913,6;
  PLA 1,24 → 3,61 g); relleno 0 y 100; pieza delgada (shell ≥ volumen);
  `parts` con OpenSCAD real sobre `nameplate-two-color.scad` (2 partes, colores
  y volúmenes que suman el total); middleware de partes con rutas maliciosas.
- 7.B: banco + `uishot-fit.sh`; prueba de clic por CDP en el banco.
- Nadie ejecuta Bambu Studio ni abre sesiones de agentes.
