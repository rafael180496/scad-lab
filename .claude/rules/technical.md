# Restricciones técnicas (no negociables)

1. **Sin CGO.** Todo Go puro; Windows se cross-compila desde macOS.
2. **macOS (Apple Silicon) y Windows (x64)** son plataformas de primera. Nada
   que funcione solo en una (rutas, procesos, atajos Cmd/Ctrl, PATH).
3. **Checkpoints inmutables.** Ningún código de ScadLab sobrescribe ni borra un
   `.scad` de un workspace. «Borrar» mueve a `.scadlab/trash/`.
4. **Migraciones aditivas** de `scadlab.db` (ver `claude/07-database.md`).
5. **Sin secretos en disco** gestionado por ScadLab: API keys por variable de
   entorno.
6. **Sin dependencias de red en runtime para la UI**: nada de CDN (Monaco,
   fuentes, iconos van empaquetados). La app tiene que abrir sin internet.
7. **OpenSCAD es externo**: nunca se asume una versión; se detecta y se degrada
   (Manifold → CGAL, 3MF con color → 3MF por objetos).
8. **Binario < 100 MB.**
