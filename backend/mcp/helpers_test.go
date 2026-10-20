package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/appdata"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Estas pruebas hablan con el servidor por io.Pipe, como lo haría un agente por
// stdio, sobre un workspace temporal y con OpenSCAD real (las que lo necesitan
// se saltan si no está instalado). La base va en un SCADLAB_DATA_DIR temporal:
// jamás se toca la de la app.

func needOpenSCAD(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if !detect.OpenSCAD(ctx, "").Found {
		t.Skip("OpenSCAD no está instalado")
	}
}

// client es un cliente MCP de prueba. Cada línea que el servidor escribe en
// stdout se registra y se exige que sea JSON-RPC: es la comprobación de «stdout
// limpio».
type client struct {
	t     *testing.T
	in    *io.PipeWriter
	msgs  chan map[string]any
	done  chan error
	cwg   sync.WaitGroup
	mu    sync.Mutex
	lines []string
	next  int
	// Ws es la carpeta del workspace del servidor.
	Ws string
}

// setup crea el workspace y el directorio de datos temporales y arranca el
// servidor. seed, si no es nil, prepara la base antes (settings, workspaces).
func setup(t *testing.T, seed func(t *testing.T, st *store.Store, ws string)) *client {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv(appdata.OverrideEnv, dataDir)
	t.Cleanup(func() { i18n.SetLang("en") })
	i18n.SetLang("en")

	ws, err := workspace.Create(t.TempDir(), "Prueba MCP", miniPrinter())
	if err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		dbPath, err := appdata.DBPath()
		if err != nil {
			t.Fatal(err)
		}
		st, err := store.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		seed(t, st, ws)
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, msgs: make(chan map[string]any, 64), done: make(chan error, 1), Ws: ws}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := Serve(ctx, inR, outW, Options{WorkspaceDir: ws, Version: "test", Log: io.Discard})
		outW.Close()
		c.done <- err
	}()
	c.cwg.Add(1)
	go func() {
		defer c.cwg.Done()
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := sc.Text()
			c.mu.Lock()
			c.lines = append(c.lines, line)
			c.mu.Unlock()
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Errorf("stdout no es JSON: %q (%v)", truncate(line), err)
				continue
			}
			if m["jsonrpc"] != "2.0" {
				t.Errorf("stdout no es JSON-RPC 2.0: %q", truncate(line))
			}
			c.msgs <- m
		}
		close(c.msgs)
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case err := <-c.done:
			if err != nil {
				t.Errorf("Serve devolvió error: %v", err)
			}
		case <-time.After(30 * time.Second):
			cancel()
			t.Error("Serve no terminó al cerrar stdin")
		}
		cancel()
		c.cwg.Wait()
		c.mu.Lock()
		n := len(c.lines)
		c.mu.Unlock()
		if n == 0 {
			t.Error("el servidor no escribió nada")
		}
	})
	return c
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func (c *client) send(v any) {
	c.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.in.Write(append(raw, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() map[string]any {
	c.t.Helper()
	select {
	case m, ok := <-c.msgs:
		if !ok {
			c.t.Fatal("el servidor cerró stdout")
		}
		return m
	case <-time.After(120 * time.Second):
		c.t.Fatal("tiempo de espera de la respuesta")
	}
	return nil
}

// call manda una petición y devuelve su respuesta completa.
func (c *client) call(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	id := c.next
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	m := c.recv()
	if got, _ := m["id"].(float64); int(got) != id {
		c.t.Fatalf("id de respuesta %v, quería %d: %v", m["id"], id, m)
	}
	return m
}

// toolResp es el resultado de una herramienta, ya desarmado.
type toolResp struct {
	IsError bool
	Text    string
	Content []map[string]any
}

func (c *client) tool(name string, args map[string]any) toolResp {
	c.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	m := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	if m["error"] != nil {
		c.t.Fatalf("%s: error JSON-RPC: %v", name, m["error"])
	}
	res, _ := m["result"].(map[string]any)
	r := toolResp{}
	r.IsError, _ = res["isError"].(bool)
	items, _ := res["content"].([]any)
	var texts []string
	for _, it := range items {
		mm, _ := it.(map[string]any)
		r.Content = append(r.Content, mm)
		if mm["type"] == "text" {
			s, _ := mm["text"].(string)
			texts = append(texts, s)
		}
	}
	r.Text = strings.Join(texts, "\n")
	return r
}

// toolJSON llama a la herramienta y decodifica su texto como JSON.
func (c *client) toolJSON(name string, args map[string]any) (map[string]any, toolResp) {
	c.t.Helper()
	r := c.tool(name, args)
	var out map[string]any
	if err := json.Unmarshal([]byte(r.Text), &out); err != nil {
		c.t.Fatalf("%s: el resultado no es JSON: %v\n%s", name, err, truncate(r.Text))
	}
	return out, r
}

func (c *client) mustOK(name string, args map[string]any) map[string]any {
	c.t.Helper()
	out, r := c.toolJSON(name, args)
	if r.IsError {
		c.t.Fatalf("%s: isError inesperado: %s", name, truncate(r.Text))
	}
	return out
}

// create crea un checkpoint y devuelve su nombre.
func (c *client) create(label, source string, extra map[string]any) string {
	c.t.Helper()
	args := map[string]any{"label": label, "source": source}
	for k, v := range extra {
		args[k] = v
	}
	out := c.mustOK("create_checkpoint", args)
	name, _ := out["file"].(string)
	if name == "" {
		c.t.Fatalf("create_checkpoint no devolvió archivo: %v", out)
	}
	return name
}

func (c *client) readWs(rel string) string {
	c.t.Helper()
	raw, err := os.ReadFile(filepath.Join(c.Ws, rel))
	if err != nil {
		c.t.Fatal(err)
	}
	return string(raw)
}

func (c *client) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(c.Ws, rel))
	return err == nil
}

const (
	srcCube   = "color(\"red\") cube([20, 10, 5]);\n"
	srcBroken = "// intento roto\ncolor(\"red\") cube([20, 10, 5];\ncube(1);\n"
)

// miniPrinter es la impresora de las fixtures: la A1 mini del catálogo.
func miniPrinter() printers.Printer {
	p, _ := printers.ByID("bambu-a1-mini")
	return p
}
