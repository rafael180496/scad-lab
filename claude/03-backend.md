# Backend — reglas Go

Skills a aplicar: `golang-code-style`, `golang-design-patterns`,
`golang-performance`, `golang-concurrency`.

- **Sin CGO.** SQLite es `modernc.org/sqlite`. Windows se cross-compila desde
  macOS (`./scripts/package-windows.sh`); una dependencia con CGO lo rompe.
- **Multiplataforma siempre.** Rutas con `filepath.Join`; nombres de carpeta
  con `workspace.Slug` (Windows prohíbe `<>:"/\|?*` y `CON`, `NUL`…); nada de
  `/tmp` fijo (`os.TempDir()`); finales de línea `\r\n` tolerados al leer.
- **Procesos hijos**: `exec.CommandContext` con plazo o cancelación, sin shell
  intermedia, `hideWindow(cmd)` en Windows (si no, cada llamada abre una consola).
- **Ejecutables**: resolver con `backend/detect.Find`. Una app lanzada desde el
  Finder o el menú Inicio no hereda el PATH de la shell.
- **Errores visibles**: `i18n.New/Errorf(i18n.Msg{ES, EN})`. Nunca un texto
  visible solo en un idioma.
- **Secretos**: nunca en la base ni en logs. Los proveedores de IA guardan el
  *nombre* de la variable de entorno (`apiKeyEnv`).
- **Tests** con `t.TempDir()`; jamás contra el directorio de datos real
  (`SCADLAB_DATA_DIR` existe para eso).
- `go vet ./...` limpio y `gofmt` aplicado.
