package mcpconf

import (
	"reflect"
	"testing"
)

func TestAgyArgs(t *testing.T) {
	bin := "/Applications/My Apps/ScadLab.app/Contents/MacOS/ScadLab"
	if got, want := AgyAddArgs(bin), []string{"mcp", "add", "--", "scadlab", bin, "--mcp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("add: %q", got)
	}
	if got, want := AgyRemoveArgs(), []string{"mcp", "remove", "scadlab"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remove: %q", got)
	}
	if got, want := AgyListArgs(), []string{"mcp", "list"}; !reflect.DeepEqual(got, want) {
		t.Errorf("list: %q", got)
	}
	// Nada de --workspace: una entrada global sirve a todos los workspaces.
	for _, a := range AgyAddArgs(bin) {
		if a == "--workspace" {
			t.Error("la entrada global no lleva --workspace")
		}
	}
}

// Salida real de agy 1.3.6 (verificada con un HOME temporal).
const agyListReal = `NAME     TYPE   STATUS    COMMAND/URL
other    stdio  enabled   /bin/x -y a b --z
probe    stdio  disabled  echo x
scadlab  stdio  enabled   /Applications/My Apps/ScadLab.app/Contents/MacOS/ScadLab --mcp
web      http   enabled   https://example.com/mcp
`

func TestParseAgyList(t *testing.T) {
	rows := ParseAgyList(agyListReal)
	want := []AgyEntry{
		{"other", "stdio", true, "/bin/x -y a b --z"},
		{"probe", "stdio", false, "echo x"},
		{"scadlab", "stdio", true, "/Applications/My Apps/ScadLab.app/Contents/MacOS/ScadLab --mcp"},
		{"web", "http", true, "https://example.com/mcp"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("filas:\n got %+v\nwant %+v", rows, want)
	}
	for _, empty := range []string{"", "No MCP servers configured.\n", "NAME TYPE STATUS COMMAND/URL\n", "Error: algo\r\n"} {
		if r := ParseAgyList(empty); len(r) != 0 {
			t.Errorf("%q: %+v", empty, r)
		}
	}
	// Con CRLF (Windows) también.
	if r := ParseAgyList("NAME TYPE STATUS COMMAND/URL\r\nscadlab  stdio  enabled  C:\\Apps\\ScadLab.exe --mcp\r\n"); len(r) != 1 || r[0].Command != `C:\Apps\ScadLab.exe --mcp` {
		t.Errorf("crlf: %+v", r)
	}
}

func TestAgyStateOf(t *testing.T) {
	bin := "/Applications/My Apps/ScadLab.app/Contents/MacOS/ScadLab"
	rows := ParseAgyList(agyListReal)
	if s, e := AgyStateOf(rows, bin); s != AgyOK || e == nil {
		t.Errorf("ok: %s", s)
	}
	if s, _ := AgyStateOf(rows, "/Nuevo/ScadLab"); s != AgyOther {
		t.Errorf("otra ruta: %s", s)
	}
	if s, _ := AgyStateOf(ParseAgyList("scadlab stdio enabled "+bin+" --mcp --workspace /x\n"), bin); s != AgyOther {
		t.Errorf("con --workspace no es la entrada global: %s", s)
	}
	if s, _ := AgyStateOf(ParseAgyList("scadlab stdio disabled "+bin+" --mcp\n"), bin); s != AgyDisabled {
		t.Errorf("desactivada: %s", s)
	}
	if s, e := AgyStateOf(ParseAgyList("No MCP servers configured.\n"), bin); s != AgyMissing || e != nil {
		t.Errorf("sin entrada: %s", s)
	}
	if s, _ := AgyStateOf(ParseAgyList("probe stdio enabled echo x\n"), bin); s != AgyMissing {
		t.Errorf("otro servidor: %s", s)
	}
}
