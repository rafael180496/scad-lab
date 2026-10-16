package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/appdata"
)

// runMCP en proceso: stdin, stdout y stderr son buffers. El modo --mcp no abre
// ninguna ventana (corre antes de wails.Run), así que se puede probar sin GUI.
func TestRunMCPInProcess(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	ws := t.TempDir()
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out, errOut bytes.Buffer
	if code := runMCP([]string{"--workspace", ws}, in, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	n := 0
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m["jsonrpc"] != "2.0" {
			t.Fatalf("stdout no es JSON-RPC: %q", sc.Text())
		}
		n++
	}
	if n != 2 {
		t.Errorf("respuestas: %d (la notificación no lleva respuesta)", n)
	}
}

func TestRunMCPFailures(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	var out, errOut bytes.Buffer
	if code := runMCP([]string{"--workspace", filepath.Join(t.TempDir(), "no-existe")}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Errorf("workspace inexistente: exit %d", code)
	}
	if !strings.Contains(errOut.String(), "scadlab --mcp") || out.Len() != 0 {
		t.Errorf("el error va a stderr y stdout queda vacío: out=%q err=%q", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := runMCP([]string{"--no-existe"}, strings.NewReader(""), &out, &errOut); code != 2 || out.Len() != 0 {
		t.Errorf("flag desconocido: exit %d out=%q", code, out.String())
	}
}

// Prueba de humo del binario real: se compila y se lanza con --mcp. Compilar la
// app entera tarda, así que solo corre con SCADLAB_MCP_SMOKE=1:
//
//	SCADLAB_MCP_SMOKE=1 go test -run TestMCPBinarySmoke -v .
func TestMCPBinarySmoke(t *testing.T) {
	if os.Getenv("SCADLAB_MCP_SMOKE") == "" {
		t.Skip("prueba de humo del binario: SCADLAB_MCP_SMOKE=1 para correrla")
	}
	bin := filepath.Join(t.TempDir(), "scadlab-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ws := t.TempDir()
	cmd := exec.Command(bin, "--mcp", "--workspace", ws)
	cmd.Env = append(os.Environ(), appdata.OverrideEnv+"="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n")
	io.WriteString(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	got := map[int]map[string]any{}
	deadline := time.AfterFunc(30*time.Second, func() { _ = cmd.Process.Kill() })
	defer deadline.Stop()
	for len(got) < 2 && sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stdout no es JSON: %q", sc.Text())
		}
		id, _ := m["id"].(float64)
		got[int(id)] = m
	}
	stdin.Close()
	if err := <-done; err != nil {
		t.Errorf("el proceso terminó con error: %v\n%s", err, stderr.String())
	}
	init, _ := got[1]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" {
		t.Errorf("initialize: %v (stderr: %s)", got[1], stderr.String())
	}
	tools, _ := got[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 12 {
		t.Errorf("tools/list: %d herramientas", len(tools))
	}
}
