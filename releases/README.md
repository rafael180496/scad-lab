# releases/

Zona de los artefactos **publicables** de ScadLab: `macos/ScadLab-vX.Y.Z.dmg` y
`windows/ScadLab-vX.Y.Z-windows-amd64.exe`, cada uno con su `README.md`
(versión, SHA-256, compatibilidad, instalación).

Runbook completo: [.claude/specs/releases.md](../.claude/specs/releases.md).
Resumen:

```bash
./scripts/bump-version.sh patch     # VERSION 0.1.0 → 0.1.1
./scripts/package-all.sh            # .dmg + .exe en build/bin/
cp build/bin/ScadLab-v0.1.1.dmg releases/macos/
cp build/bin/ScadLab-v0.1.1-windows-amd64.exe releases/windows/
shasum -a 256 releases/*/ScadLab-v0.1.1*   # → a cada README
# CHANGELOG.md: [Unreleased] → [0.1.1] - AAAA-MM-DD ; index.html al día
# commit + tag v0.1.1 los hace el usuario; el workflow publica el Release
```
