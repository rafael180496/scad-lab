package detect

// ExtraDirs son los directorios de instalación habituales de los CLIs que una
// app gráfica no tiene en su PATH (ver extraDirs). La terminal integrada los
// agrega al PATH del proceso que lanza: un CLI instalado con npm es un script
// con `#!/usr/bin/env node`, y sin el directorio de node en el PATH muere con
// «env: node: No such file or directory» aunque el propio CLI sí se haya
// encontrado.
func ExtraDirs() []string { return extraDirs() }

// CLIPath es la ruta del ejecutable de un CLI agéntico (o de uno de sus
// nombres anteriores, ver altCommands), o "" si no está. A diferencia de CLI no
// corre `--version`: lanzar una terminal no debe esperar ~1 s a que arranque
// node solo para saber dónde está el comando.
func CLIPath(id, command string) string {
	if p := Find(command); p != "" {
		return p
	}
	for _, alt := range altCommands[id] {
		if p := Find(alt); p != "" {
			return p
		}
	}
	return ""
}
