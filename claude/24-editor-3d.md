# Editor 3D de un STL: la interfaz (S13, 13.2)

Spec: [.claude/specs/s13-edicion-stl.md](../.claude/specs/s13-edicion-stl.md). El núcleo Go, el contrato `Scene` y la
convención de rotación están en [23-edicion-stl.md](23-edicion-stl.md); aquí, lo que hace el frontend con eso.

## Mapa de archivos

```
lib/meshEditMath.ts      matemática pura (solo three): pose ↔ matriz, centrar/apoyar/espejar/cara, plano de corte. Probada con Node
lib/meshEditSession.ts   sesión de edición: store de módulo, pila de operaciones, deshacer, toScene (a Go). SIN three
lib/meshEditAI.ts        «Modificar con IA»: PrepareMeshEdit + AddChatAttachment + borrador del chat (sessionBridge)
lib/mesh.ts              loadRawMesh/cachedRawMesh: la malla del archivo SIN centrar (clave raw:<clave>)
hooks/useModelImport.ts  botón (ChooseModelFile) y arrastre (OnFileDrop) → un solo diálogo
components/meshedit/     ImportDialog · MeshEditor (lazy) · EditToolbar · EditViewport · EditScene · SidePanel
                         NumField · EditDialogs (Aplicar y Ver código) · actions.ts (las operaciones) · live.ts
```

`MeshEditor` entra con `React.lazy` desde `RenderPanel` (otro chunk: gizmo, escena y paneles). El store de la sesión y el
diálogo de importar están en el chunk principal y no importan three.

## Sesión y pila de operaciones

- `meshEditStore` guarda **una sesión por workspace** (`Session`: checkpoint, `ref`, `meshPath`, hechos de la malla, `base`,
  `entries`, `index`, selección, herramienta, ajuste a pasos). Vive en un store de módulo y no en un componente por dos
  razones: la escena está en **otra raíz de React** (el `<Canvas>` no comparte contexto con la interfaz) y la edición tiene
  que sobrevivir al cambio de pestaña de proyecto. **No se persiste**: cerrar la app o el proyecto la descarta.
- La pila es una lista de **documentos completos** (`Doc{model, color, parts, cut}`), no de diffs: deshacer, rehacer e ir a
  un paso son mover `index`. `commit(ws, doc, label, key)` descarta lo rehacible y **funde** dos operaciones seguidas con la
  misma `key` (`move:model`, `move:p2`, `color:model`…); una `key` vacía no se funde.
- Las etiquetas (`t.meshEdit.ops.*`, en el idioma de la interfaz al hacerlas) viajan en `Scene.ops` y son los comentarios
  del código. `toScene` manda el estado final: Go genera el código; la misma escena da el mismo código.
- Cambiar de checkpoint con la edición abierta pasa por `confirmLeaveMeshEdit` (en `App.openCheckpoint`); cerrar una
  pestaña de proyecto cuenta una edición con cambios entre lo que se pierde. `RenderPanel` cierra una sesión cuyo
  checkpoint ya no es el elegido.

## La escena (`EditScene.tsx`)

- Coordenadas del mundo = las de OpenSCAD (Z arriba). La pose usa `position`, `rotation` con orden **`'ZYX'`** y `scale`
  con el signo del espejo: la convención de [23](23-edicion-stl.md#convención-de-rotación-contrato-con-el-frontend), con su
  prueba en `frontend/scripts/mesh-edit.test.mjs` (contra la fórmula de OpenSCAD escrita a mano).
- **El gizmo trabaja sobre el centro de la caja**, no sobre el origen del archivo (un STL suele traerlo lejos de la pieza y
  girar sobre él mandaba la pieza a pasear). `Bodies` coloca el grupo del gizmo en `T + R·S·pivote` y corre la malla
  `−pivote` dentro; `poseFromPivoted` deshace la cuenta al soltar. El contrato sigue siendo la pose sobre el origen.
- **El gizmo agarra el objeto de verdad** (`TransformControls` de drei, con los controles de órbita `makeDefault` en `Rig`).
  Mientras se arrastra, `liveStore` lleva los valores a Propiedades y a las medidas (con `requestAnimationFrame`); al soltar
  (`mouseUp`) se confirma **una** operación. La rotación 'world' y la escala 'local' (la del eje del objeto, como `scale()`).
- **Escalar** deja la pose con escala absoluta (el espejo es una opción aparte) y mínimo 0,001. **Espejar** conserva el
  centro de la caja. El plano de corte no se escala: con la herramienta Escalar su gizmo pasa a mover.
- **Agujeros**: volumen rojo translúcido (`theme.danger`); el booleano real es el render al aplicar. **Texto**: caja con el
  texto dibujado encima (`CanvasTexture`); la forma exacta la da OpenSCAD.
- **Corte**: planos de recorte de la GPU (`material.clippingPlanes` + `gl.localClippingEnabled`), sin tocar la malla; las
  caras de atrás del modelo, más oscuras, hacen de tapa. «Partir»: dos variantes del cuerpo (modelo y uniones; los
  agujeros no se copian), como colores (cada una con el suyo) o como objetos (la de abajo corrida `ancho + 10 mm` en X, como
  el código generado).
- **Clic**: un solo rayo al soltar (`PickLayer`, como `Parts` del visor: nada de `onClick` de R3F, que hace raycast en cada
  movimiento). El clic sobre una flecha del gizmo (`axis` del `TransformControls`) no selecciona. El plano de corte solo se
  elige si no hay otra cosa detrás. «Apoyar sobre una cara»: clic en la cara → su normal en el mundo (matriz normal, vale con
  espejo) → `layFaceDown`; el resaltado de la cara bajo el puntero se omite por encima de 250 000 triángulos.

## Atajos del modo Editar

Con el foco dentro del editor (se enfoca al entrar): `W`/`E`/`R` mover/rotar/escalar, `⌘Z` / `⌘⇧Z` (Ctrl; también `Ctrl+Y` en
Windows), `Supr`/`⌫` quita la pieza elegida, `1`–`7` y `F` como el visor, `Esc` cancela «Cara» o, si no, descarta (con
confirmación si hay cambios). El listener es del propio editor (`onKeyDown` de su raíz), **no** de `window`: no pisa a
Monaco ni a la terminal. En un campo con un borrador sin confirmar (`data-dirty` de `NumField`) `⌘Z` es el del texto. Un
`NumField` confirma con Intro o al salir; las flechas ↑/↓ suman un paso (Shift ×10, Alt ×0,1).

## Importar y «Modificar con IA»

- `useModelImport` (en `App`): el botón de la barra lateral (`ChooseModelFile`) y `OnFileDrop(cb, false)` de Wails —`main.go`
  ya activa `DragAndDrop` con `DisableWebViewDrop`, así que el arrastre HTML5 no llega y es el runtime quien entrega las
  **rutas**—. Sin runtime (banco de capturas) el `try` lo ignora. Un archivo que no es STL/3MF/OBJ avisa; con varios se importa
  el primero. El diálogo muestra `AnalyzeModelFile` y sus avisos; `ImportModel` crea el checkpoint raíz y `adoptNewCheckpoint`
  (el mismo camino que guardar del editor) lo elige.
- «Modificar con IA» (`lib/meshEditAI.ts`): `PrepareMeshEdit` → `AddChatAttachment(ws, <ruta>/<ref>)` → `requestChatDraft`
  (`sessionBridge`, igual que `openChat`: el chat entra con lazy y el borrador queda pendiente hasta que se monta). `ChatPanel`
  abre un chat **nuevo**, escribe «Modifica este modelo: » y suma el adjunto (`useChatAttachments.addReady`). Se toma con un
  `setTimeout(0)` para sobrevivir al montaje doble de `StrictMode`, cuyo desmontaje suelta los adjuntos.
- **Límite conocido**: `AddChatAttachment` copia con `CopyToRefs`, que nunca pisa: sobre un archivo que ya está en `refs/`
  deja una copia `-2` (y quitar el chip la borra). Lo correcto sería que Go reutilice el archivo si ya está en `refs/`.

## Paneles angostos

`useNarrow` sobre la raíz del editor: bajo 960 px la barra omite los rótulos; bajo 640 px el panel lateral pasa a **cajón con
pestañas** bajo el visor (a lo ancho van Propiedades y Pila a la vez) y las medidas se reducen a la selección. La barra y el
pie envuelven (`flex-wrap`), nunca desbordan.

## Banco de capturas y pruebas

- Vistas `mesh-import`, `mesh-edit`, `mesh-cut`, `mesh-hole` y `mesh-ai` (filas de `help-shots.sh`; `mesh-cut` a 1440×1000), y
  `mesh-edit-w320…w1100` (el editor solo, de ese ancho, para `uishot-fit.sh`). El banco usa `ShotOptions.meshEdit`
  (`MeshEditShot`: pasos con las **mismas operaciones** que los clics, herramienta, selección y vista), `meshImport` y `meshAI`.
  Malla de ejemplo: `public/uishot/enclosure.stl` (la caja de la plantilla, con la esquina en el origen: justo el caso que el
  pivote resuelve). El build borra `dist/uishot`.
- `uishot-fit.sh` falla si la barra, el panel lateral, el pie o el editor (`data-meshedit-*`) tienen contenido más ancho que su caja.
- `cd frontend && pnpm test` incluye `mesh-edit.test.mjs`: la matriz ZYX contra la fórmula de OpenSCAD (también con espejo y
  escala), centrar, apoyar, espejar, apoyar sobre una cara, rotación sobre un pivote y el plano de recorte.
- Sin probar en la ventana real de Wails: el arrastre de archivos (`OnFileDrop`) y el diálogo nativo `ChooseModelFile`.
