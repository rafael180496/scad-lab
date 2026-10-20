# Restricciones técnicas (no negociables)

1. **Sin CGO.** Todo Go puro; Windows se cross-compila desde macOS.
2. **macOS (Apple Silicon) y Windows (x64)** son plataformas de primera. Nada
   que funcione solo en una (rutas, procesos, atajos Cmd/Ctrl, PATH).
3. **Checkpoints protegidos: se reescriben solo con respaldo, y ninguna herramienta
   borra.** Ningún código de ScadLab sobrescribe un `.scad` de un workspace sin guardar
   antes lo que había, ni lo borra: «borrar» mueve a `.scadlab/trash/`. Las excepciones
   son cuatro, acotadas y probadas: (a) `checkpoint.SetParent` reescribe **solo el valor
   de `parent=`** de la línea de cabecera (metadato del árbol, no el modelo; atómico,
   conserva el resto byte a byte, permisos y fecha); (b) la **eliminación permanente de
   la papelera** (`DeleteTrashed` / `EmptyTrash`) la dispara únicamente el usuario desde
   la interfaz, con confirmación, y **nunca está expuesta a un agente** (ni MCP ni
   Hermes); (c) la **reescritura en el lugar con respaldo previo** en
   `.scadlab/history/<archivo>/`: `update_checkpoint` (la IA, **solo en «Agente»**;
   `checkpoint.Update`) y «Restaurar versión» / «Deshacer cambio de la IA» (el usuario;
   `checkpoint.RestoreVersion`, que guarda primero la versión actual). El orden es
   siempre respaldo → escritura atómica: si el respaldo falla no se escribe; (d) la
   **poda del historial** por el tope (`Settings.HistoryMaxVersions`, 20 de fábrica),
   que borra **respaldos** —copias de seguridad—, nunca un checkpoint. Las escrituras
   directas con las herramientas de archivo de un CLI no dejan respaldo y las reglas se
   lo prohíben a la IA para `.scad`. Ver `claude/15-checkpoints.md`.
4. **Migraciones aditivas** de `scadlab.db` (ver `claude/07-database.md`).
5. **Sin secretos en texto plano**: las claves de API se cifran con AES-256-GCM con
   una llave derivada (argon2id) de la contraseña maestra; la llave solo se recuerda
   en el llavero del SO si el usuario lo elige; o por variable de entorno.
6. **Sin dependencias de red en runtime para la UI**: nada de CDN (Monaco,
   fuentes, iconos van empaquetados). La app tiene que abrir sin internet.
7. **OpenSCAD nativo es externo y preferido**: nunca se asume una versión; se
   detecta y se degrada (Manifold → CGAL, 3MF con color → 3MF por objetos).
   Desde S11 hay **un** respaldo integrado: el OpenSCAD oficial compilado a
   WebAssembly (versión fijada, sin modificar, GPL, ver
   `THIRD_PARTY_NOTICES.md`), que solo corre si no se encontró el nativo. El
   OpenSCAD descargado desde la app va a `<datos>/tools/openscad/` con SHA-256
   fijado en el código.
8. **Binario < 100 MB.**
9. **Ningún test ni script del repo abre slicers ni otras aplicaciones del
   usuario** (Bambu Studio, OrcaSlicer, navegadores visibles). **ScadLab sí se
   puede abrir** para probarlo (arranque real, prueba de humo); lo que nunca se
   dispara es su botón «Abrir en Bambu Studio». Bambu
   Studio por línea de comandos se cae y le abre al usuario el diálogo de macOS
   «se cerró inesperadamente» en cada corrida (pasó en S9). Por pedido del
   usuario **no hay pruebas automáticas con Bambu Studio en el repo**, ni con
   build tag: la exportación 3MF la prueba él a mano en Bambu Studio. La app
   **sí** puede abrir un archivo en Bambu Studio cuando el usuario lo pide con
   un clic («Abrir en Bambu Studio»): se abre la app con el archivo, nunca su
   CLI, y en los tests el lanzador se inyecta y no ejecuta nada. OpenSCAD
   por CLI sí está permitido (no tiene GUI en ese modo). Chrome solo en
   `--headless`.
