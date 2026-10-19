# Restricciones técnicas (no negociables)

1. **Sin CGO.** Todo Go puro; Windows se cross-compila desde macOS.
2. **macOS (Apple Silicon) y Windows (x64)** son plataformas de primera. Nada
   que funcione solo en una (rutas, procesos, atajos Cmd/Ctrl, PATH).
3. **Checkpoints inmutables.** Ningún código de ScadLab sobrescribe ni borra un
   `.scad` de un workspace. «Borrar» mueve a `.scadlab/trash/`.
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
