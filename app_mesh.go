package main

import (
	"path/filepath"
	"sync"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/meshserve"
)

// meshInfoCacheMax acota la caché de GetMeshInfo. Una entrada pesa ~100 bytes:
// el tope no es por memoria sino para que una sesión de días no crezca sin
// límite. Al pasarlo se vacía entera (recalcular una es leer un STL).
const meshInfoCacheMax = 4096

// meshInfos cachea mesh.Info por clave. La clave es el sha256 del modelo, así
// que su malla nunca cambia de contenido y la entrada no se invalida nunca.
// Vive a nivel de paquete y no en App porque app.go no es de este bloque (S2
// 2.A); al ser por contenido, compartirla entre instancias de App es correcto.
var meshInfos = struct {
	sync.Mutex
	m map[string]mesh.Info
}{m: map[string]mesh.Info{}}

// GetMeshInfo mide una malla de la caché de render: tamaño, volumen, área,
// triángulos y si es cerrada. meshPath es render.Result.MeshPath; solo se
// aceptan archivos de la caché (<datos>/cache/meshes/<clave>.stl), nunca una
// ruta arbitraria.
func (a *App) GetMeshInfo(meshPath string) (mesh.Info, error) {
	if a.dataDir == "" {
		return mesh.Info{}, i18n.Errorf(i18n.Msg{ES: "el directorio de datos no está disponible: %v", EN: "the data directory is not available: %v"}, a.initErr)
	}
	cacheDir := filepath.Join(a.dataDir, "cache", "meshes")
	name := filepath.Base(meshPath)
	if filepath.Dir(meshPath) != cacheDir || !meshserve.ValidName(name) {
		return mesh.Info{}, i18n.Errorf(i18n.Msg{ES: "%q no es una malla de la caché de render", EN: "%q is not a mesh from the render cache"}, meshPath)
	}

	meshInfos.Lock()
	in, ok := meshInfos.m[name]
	meshInfos.Unlock()
	if ok {
		return in, nil
	}

	// Se mide fuera del candado: un STL grande tarda, y otra malla no tiene
	// por qué esperar. Dos pedidos simultáneos de la misma clave la miden dos
	// veces y guardan lo mismo.
	in, err := mesh.InfoFromFile(filepath.Join(cacheDir, name))
	if err != nil {
		return mesh.Info{}, i18n.Errorf(i18n.Msg{ES: "no se pudo analizar la malla: %w", EN: "could not analyze the mesh: %w"}, err)
	}
	meshInfos.Lock()
	if len(meshInfos.m) >= meshInfoCacheMax {
		meshInfos.m = map[string]mesh.Info{}
	}
	meshInfos.m[name] = in
	meshInfos.Unlock()
	return in, nil
}
