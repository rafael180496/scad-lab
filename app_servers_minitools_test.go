package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Todas las pruebas usan un vault.db FALSO creado acá, en una carpeta temporal
// que SCADLAB_MINITOOLS_DIR hace pasar por la de mini-tools. Nunca se lee el
// directorio real de mini-tools.

const miniSecret = "ENC-SECRETO-DE-MINI-TOOLS"

// fakeMiniToolsVault crea un vault.db con el esquema de model_servers de
// mini-tools (con sus columnas cifradas llenas de un centinela) y lo deja como
// la carpeta de mini-tools.
func fakeMiniToolsVault(t *testing.T, full bool) (dir string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv(MiniToolsDirEnv, dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := `CREATE TABLE model_servers (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, kind TEXT NOT NULL, base_url TEXT NOT NULL,
		encrypted_token BLOB, token_nonce BLOB, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		insecure_tls INTEGER NOT NULL DEFAULT 0, encrypted_headers BLOB, headers_nonce BLOB,
		context_tokens INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1, default_model TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0)`
	if !full {
		// Un vault de una versión vieja: sin modelo por defecto ni ventana de contexto.
		schema = `CREATE TABLE model_servers (
			id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, kind TEXT NOT NULL, base_url TEXT NOT NULL,
			encrypted_token BLOB, token_nonce BLOB, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	insert := func(id, name, kind, url, def string, ctx int) {
		if full {
			_, err = db.Exec(`INSERT INTO model_servers (id, name, kind, base_url, encrypted_token, token_nonce, created_at, updated_at, encrypted_headers, headers_nonce, default_model, context_tokens)
				VALUES (?, ?, ?, ?, ?, ?, 1, 1, ?, ?, ?, ?)`, id, name, kind, url, []byte(miniSecret), []byte(miniSecret), []byte(miniSecret), []byte(miniSecret), def, ctx)
		} else {
			_, err = db.Exec(`INSERT INTO model_servers (id, name, kind, base_url, encrypted_token, token_nonce, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, 1)`,
				id, name, kind, url, []byte(miniSecret), []byte(miniSecret))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("a", "Ollama del taller", "ollama", "http://192.168.1.5:11434", "qwen3-coder:30b", 32768)
	insert("b", "LM Studio", "openai", "http://localhost:1235/v1/", "", 0)
	insert("c", "Mi Ollama", "ollama", "http://localhost:11434", "llama3.2:3b", 0) // misma URL que «Ollama» de ScadLab
	insert("d", "anthropic api", "anthropic", "https://api.anthropic.com", "", 0)  // mismo nombre (otra capitalización) que «Anthropic API»
	insert("e", "Gemini nativo", "gemini", "https://example.com", "", 0)           // un tipo que ScadLab no tiene
	// El resto de la carpeta de mini-tools: no se abre. Sin permisos, un intento
	// de leerlos fallaría.
	for _, name := range []string{"salt.bin", "vault.db-shm"} {
		p := filepath.Join(dir, name)
		if name == "salt.bin" {
			if err := os.WriteFile(p, []byte(miniSecret), 0o000); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func dirDigest(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		st, _ := e.Info()
		h.Write([]byte(e.Name()))
		h.Write([]byte{byte(st.Size() % 251), byte(st.Size() / 251 % 251)})
		if e.Name() == "vault.db" {
			raw, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			h.Write(raw)
		}
	}
	return string(h.Sum(nil))
}

func TestImportMiniToolsNotFoundIsNotAnError(t *testing.T) {
	a := newTestApp(t)
	t.Setenv(MiniToolsDirEnv, t.TempDir())
	res, err := a.ImportMiniToolsServers(true)
	if err != nil || res.Found || res.Imported != 0 || len(res.Servers) != 0 || !strings.HasSuffix(res.Path, "vault.db") {
		t.Fatalf("%+v %v", res, err)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"servers":[]`) {
		t.Errorf("la lista viaja vacía y no null: %s", raw)
	}
}

func TestImportMiniToolsDryRunListsWithoutWriting(t *testing.T) {
	a := newTestApp(t)
	dir := fakeMiniToolsVault(t, true)
	before := dirDigest(t, dir)
	serversBefore, _ := a.store.ListModelServers()

	res, err := a.ImportMiniToolsServers(true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Found || res.Imported != 0 || res.Path != filepath.Join(dir, "vault.db") || len(res.Servers) != 5 {
		t.Fatalf("%+v", res)
	}
	byName := map[string]MiniToolsServer{}
	for _, s := range res.Servers {
		byName[s.Name] = s
	}
	taller := byName["Ollama del taller"]
	if taller.Kind != "ollama" || taller.BaseURL != "http://192.168.1.5:11434" || taller.DefaultModel != "qwen3-coder:30b" || taller.ContextTokens != 32768 || taller.Exists || taller.Error != "" {
		t.Errorf("Ollama del taller: %+v", taller)
	}
	if lm := byName["LM Studio"]; lm.Kind != "openai" || lm.Exists {
		t.Errorf("LM Studio: %+v", lm)
	}
	if !byName["Mi Ollama"].Exists {
		t.Errorf("mismo URL que «Ollama» de ScadLab: ya existe: %+v", byName["Mi Ollama"])
	}
	if !byName["anthropic api"].Exists {
		t.Errorf("mismo nombre (sin distinguir mayúsculas): ya existe: %+v", byName["anthropic api"])
	}
	if g := byName["Gemini nativo"]; g.Exists || g.Error == "" {
		t.Errorf("un tipo desconocido se explica: %+v", g)
	}

	// La vista previa no escribe: ni en ScadLab ni en la carpeta de mini-tools.
	if after, _ := a.store.ListModelServers(); len(after) != len(serversBefore) {
		t.Errorf("dryRun creó servidores: %d → %d", len(serversBefore), len(after))
	}
	if dirDigest(t, dir) != before {
		t.Error("la carpeta de mini-tools cambió")
	}
	raw, _ := json.Marshal(res)
	for _, key := range []string{`"found":true`, `"path":`, `"servers":[`, `"imported":0`, `"baseUrl":`, `"defaultModel":`, `"exists":`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("falta %s: %s", key, raw)
		}
	}
}

func TestImportMiniToolsCreatesServersWithoutKeysAndIsIdempotent(t *testing.T) {
	a := newTestApp(t)
	dir := fakeMiniToolsVault(t, true)
	before := dirDigest(t, dir)

	res, err := a.ImportMiniToolsServers(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 {
		t.Fatalf("se importan los 2 nuevos (el resto existe o no es admitido): %+v", res)
	}
	servers, _ := a.store.ListModelServers()
	var taller, lm *int
	for i, s := range servers {
		switch s.Name {
		case "Ollama del taller":
			taller = &i
			if s.Kind != "ollama" || s.BaseURL != "http://192.168.1.5:11434" || s.DefaultModel != "qwen3-coder:30b" || s.ContextTokens != 32768 || !s.Enabled || s.HasToken || s.APIKeyEnv != "" {
				t.Errorf("Ollama del taller: %+v", s)
			}
		case "LM Studio":
			lm = &i
			if s.Kind != "openai" || s.BaseURL != "http://localhost:1235/v1" || s.HasToken || !s.Enabled {
				t.Errorf("LM Studio (la URL se normaliza): %+v", s)
			}
		case "Mi Ollama", "anthropic api", "Gemini nativo":
			t.Errorf("%s no debía importarse", s.Name)
		}
	}
	if taller == nil || lm == nil {
		t.Fatalf("faltan servidores: %+v", servers)
	}
	// Las claves se vuelven a escribir en ScadLab: nada cifrado de mini-tools
	// llega a su base ni a lo que cruza hacia la interfaz.
	rawJSON, _ := json.Marshal(res)
	if strings.Contains(string(rawJSON), miniSecret) {
		t.Error("el centinela cifrado llegó al resultado")
	}
	for _, s := range servers {
		if cipher, nonce, _ := a.store.ModelServerToken(s.ID); len(cipher) > 0 || len(nonce) > 0 {
			t.Errorf("%s no debería tener clave guardada", s.Name)
		}
	}
	dbRaw, _ := os.ReadFile(filepath.Join(a.dataDir, "scadlab.db"))
	if bytes.Contains(dbRaw, []byte(miniSecret)) {
		t.Error("el centinela cifrado llegó a scadlab.db")
	}

	// Sólo lectura: la carpeta de mini-tools quedó igual (ni -wal, -shm ni journal).
	if dirDigest(t, dir) != before {
		t.Error("la carpeta de mini-tools cambió")
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if _, err := os.Stat(filepath.Join(dir, "vault.db"+suffix)); err == nil {
			t.Errorf("se creó vault.db%s", suffix)
		}
	}

	// Importar de nuevo no duplica nada.
	again, err := a.ImportMiniToolsServers(false)
	if err != nil || again.Imported != 0 {
		t.Fatalf("segunda vez: %+v %v", again, err)
	}
	for _, s := range again.Servers {
		if s.Error == "" && !s.Exists {
			t.Errorf("%s debería figurar como existente", s.Name)
		}
	}
	if after, _ := a.store.ListModelServers(); len(after) != len(servers) {
		t.Errorf("duplicó: %d → %d", len(servers), len(after))
	}
}

// Un vault de una versión vieja (sin modelo por defecto ni ventana de contexto)
// también se lee: la consulta solo nombra las columnas que existen.
func TestImportMiniToolsOldVaultWithoutOptionalColumns(t *testing.T) {
	a := newTestApp(t)
	fakeMiniToolsVault(t, false)
	res, err := a.ImportMiniToolsServers(true)
	if err != nil || len(res.Servers) != 5 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, s := range res.Servers {
		if s.DefaultModel != "" || s.ContextTokens != 0 {
			t.Errorf("%+v", s)
		}
	}
}

// La consulta NO nombra ninguna columna cifrada, y los datos cifrados de la base
// falsa no aparecen en lo que devuelve.
func TestMiniToolsQueryNeverNamesEncryptedColumns(t *testing.T) {
	for _, have := range []map[string]bool{
		{"name": true, "kind": true, "base_url": true, "default_model": true, "context_tokens": true,
			"encrypted_token": true, "token_nonce": true, "encrypted_headers": true, "headers_nonce": true, "insecure_tls": true},
		{"name": true, "kind": true, "base_url": true},
	} {
		q := strings.ToLower(miniToolsQuery(have))
		for _, banned := range []string{"encrypted", "nonce", "headers", "insecure", "token_", "select *", "*"} {
			if strings.Contains(q, banned) && banned != "token_" {
				t.Errorf("la consulta nombra %q: %s", banned, q)
			}
		}
		for _, want := range []string{"name", "kind", "base_url", "from model_servers"} {
			if !strings.Contains(q, want) {
				t.Errorf("falta %q: %s", want, q)
			}
		}
		// Solo hay un SELECT de datos: nada que escriba.
		for _, w := range []string{"insert", "update", "delete", "drop", "alter"} {
			if strings.Contains(q, w) {
				t.Errorf("la consulta escribe: %s", q)
			}
		}
	}
	full := miniToolsQuery(map[string]bool{"name": true, "kind": true, "base_url": true, "default_model": true, "context_tokens": true})
	if !strings.Contains(full, "default_model") || !strings.Contains(full, "context_tokens") {
		t.Errorf("con las columnas opcionales: %s", full)
	}
}

func TestMiniToolsDSNIsReadOnlyImmutableAndEscaped(t *testing.T) {
	dsn := miniToolsDSN("/Users/yo/Library/Application Support/mini-tools/vault.db")
	if !strings.HasPrefix(dsn, "file:") || !strings.Contains(dsn, "mode=ro") || !strings.Contains(dsn, "immutable=1") {
		t.Errorf("dsn: %s", dsn)
	}
	if strings.Contains(dsn, " ") {
		t.Errorf("el espacio de «Application Support» va escapado: %s", dsn)
	}
	if runtime.GOOS == "windows" && !strings.Contains(miniToolsDSN(`C:\Users\yo\vault.db`), "file:///C:/") {
		t.Error("ruta de Windows")
	}
}

func TestMiniToolsVaultPathUsesTheOSConfigDirOrTheOverride(t *testing.T) {
	t.Setenv(MiniToolsDirEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", home)
	p, err := miniToolsVaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, home) || filepath.Base(p) != "vault.db" || filepath.Base(filepath.Dir(p)) != "mini-tools" {
		t.Errorf("<config del SO>/mini-tools/vault.db: %s", p)
	}
	t.Setenv(MiniToolsDirEnv, "/otra/carpeta")
	if p, _ := miniToolsVaultPath(); p != filepath.Join("/otra/carpeta", "vault.db") {
		t.Errorf("override: %s", p)
	}
}

func TestImportMiniToolsCorruptVaultIsAReadableError(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	t.Setenv(MiniToolsDirEnv, dir)
	if err := os.WriteFile(filepath.Join(dir, "vault.db"), []byte("esto no es una base sqlite, para nada, "+strings.Repeat("x", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := a.ImportMiniToolsServers(false)
	if err == nil || !res.Found || res.Imported != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(err.Error(), "mini-tools") {
		t.Errorf("el error dice de qué se trata: %v", err)
	}
}
