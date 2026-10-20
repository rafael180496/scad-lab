package mcp

import (
	"strings"
	"testing"

	"scadlab/backend/clichat"
	"scadlab/backend/turnmark"
)

// En un turno de «Solo consulta» (marca viva en el workspace) el servidor rechaza
// CADA herramienta de escritura del catálogo —sin tocar el proyecto— y deja pasar la
// lectura. Al terminar el turno vuelve a escribir.
func TestServerRejectsEveryWriteToolWhileTheTurnIsQueryOnly(t *testing.T) {
	c := setup(t, nil)
	end, err := turnmark.Begin(c.Ws, turnmark.Ask)
	if err != nil {
		t.Fatal(err)
	}
	ended := false
	defer func() {
		if !ended {
			end()
		}
	}()

	for _, name := range clichat.WriteTools() {
		// Con argumentos vacíos: el rechazo es del modo, no de la validación.
		r := c.tool(name, nil)
		if !r.IsError || !strings.Contains(r.Text, "“"+name+"” tool changes the project") || !strings.Contains(r.Text, "Query only") {
			t.Errorf("%s en consulta: isError=%v %q", name, r.IsError, truncate(r.Text))
		}
	}
	if files := c.mustOK("list_checkpoints", nil)["checkpoints"]; files != nil {
		if list, _ := files.([]any); len(list) != 0 {
			t.Errorf("ninguna escritura llegó a ejecutarse: %v", list)
		}
	}
	// La lectura pasa.
	for _, name := range []string{"list_checkpoints", "printer_info", "list_templates", "list_materials"} {
		if r := c.tool(name, nil); r.IsError {
			t.Errorf("%s es de lectura y tiene que pasar: %s", name, truncate(r.Text))
		}
	}

	end()
	ended = true
	file := c.create("caja", srcCube, nil)
	if !c.exists(file) {
		t.Errorf("sin marca se escribe: %s", file)
	}
}

// Sin marca, o con una que dice «agent» o ya venció, todo pasa.
func TestServerWritesWhenThereIsNoQueryOnlyMark(t *testing.T) {
	c := setup(t, nil)
	end, err := turnmark.Begin(c.Ws, turnmark.Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if file := c.create("caja", srcCube, nil); !c.exists(file) {
		t.Errorf("en Agente se escribe: %s", file)
	}
}

// La caja en proceso (Hermes) no lee la marca: decide el modo por su cuenta.
func TestInProcessToolboxIgnoresTheTurnMark(t *testing.T) {
	tb, ws := newToolbox(t, false)
	end, err := turnmark.Begin(ws, turnmark.Ask)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if name := tb.create(t, "caja", srcCube, nil); name == "" {
		t.Error("Hermes no depende de la marca")
	}
}
