package hermes

import (
	"context"
	"strings"
	"sync"
	"testing"

	"scadlab/backend/mcp"
)

// autoSaveFake es la red de seguridad de mentira: cada llamada recibe el texto
// del turno y contesta el siguiente guion.
type autoSaveFake struct {
	mu      sync.Mutex
	texts   []string
	outcome []*AutoSaveOutcome
}

func (f *autoSaveFake) save(_ context.Context, text string) *AutoSaveOutcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	if n := len(f.texts); n <= len(f.outcome) {
		return f.outcome[n-1]
	}
	return nil
}

func okOutcome(file string) *AutoSaveOutcome {
	return &AutoSaveOutcome{File: file, Steps: []AutoStep{
		{Tool: "autosave", Args: map[string]any{"file": file}, Text: `{"file":"` + file + `"}`},
		{Tool: "render", Args: map[string]any{"file": file}, Text: `{"status":"ok"}`},
	}}
}

func failOutcome(file, errs string) *AutoSaveOutcome {
	return &AutoSaveOutcome{File: file, Feedback: errs, Steps: []AutoStep{
		{Tool: "autosave", Args: map[string]any{"file": file}, Text: `{"file":"` + file + `"}`},
		{Tool: "render", Args: map[string]any{"file": file}, Text: errs, Failed: true},
	}}
}

const blockOK = "```openscad\ncolor(\"red\") cube(10);\n```"

// Un modelo SIN tool calling que contesta con el bloque: ScadLab lo guarda y lo
// renderiza, los dos pasos salen como pasos del turno y, con el render bien, el
// turno termina ahí (un solo pedido al modelo).
func TestAutoSaveSavesTheBlockOfATextOnlyTurn(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		`ERR 400 {"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools"}}`,
		oaStream("Aquí tienes la caja.\n"+blockOK, nil, 10, 5),
	}}
	as := &autoSaveFake{outcome: []*AutoSaveOutcome{okOutcome("001-caja.scad")}}
	cfg := baseConfig(f.client(t), newFakeTools("render"))
	cfg.AutoSave = as.save
	rec := &recorder{}
	res, err := Run(context.Background(), cfg, rec.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 2 {
		t.Fatalf("pedidos al modelo: %d", f.count())
	}
	if len(as.texts) != 1 || !strings.Contains(as.texts[0], "cube(10)") {
		t.Fatalf("la red recibe el texto del turno: %q", as.texts)
	}
	if len(res.Steps) != 2 || res.Steps[0].Tool != "autosave" || res.Steps[1].Tool != "render" || res.Steps[0].Status != StepOK || res.Steps[1].Index != 2 {
		t.Fatalf("pasos: %+v", res.Steps)
	}
	if res.Steps[0].TextAt != utf16Len(res.Text) {
		t.Errorf("los pasos van al final del texto: %d de %d", res.Steps[0].TextAt, utf16Len(res.Text))
	}
	rec.mu.Lock()
	events := len(rec.steps)
	rec.mu.Unlock()
	if events != 2 {
		t.Errorf("eventos de paso: %d", events)
	}
}

// Si el render falla y quedan pasos, el modelo recibe los errores con su línea y
// corrige: el segundo bloque se guarda como hijo del primero y termina bien.
func TestAutoSaveFeedsRenderErrorsBackToTheModel(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("Primer intento:\n```openscad\ncube([10,10,10);\n```", nil, 10, 5),
		oaStream("Corregido:\n"+blockOK, nil, 10, 5),
	}}
	as := &autoSaveFake{outcome: []*AutoSaveOutcome{
		failOutcome("001-caja.scad", `error: {"diagnostics":[{"severity":"error","line":1,"message":"syntax error"}]}`),
		okOutcome("002-caja.scad"),
	}}
	cfg := baseConfig(f.client(t), nil)
	cfg.AutoSave = as.save
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 2 || len(as.texts) != 2 {
		t.Fatalf("pedidos %d, intentos %d", f.count(), len(as.texts))
	}
	// El modelo vio el error CON su línea y el nombre del archivo.
	last := f.messages(1)
	msg, _ := last[len(last)-1]["content"].(string)
	if !strings.Contains(msg, "001-caja.scad") || !strings.Contains(msg, `"line":1`) || !strings.Contains(msg, "syntax error") {
		t.Errorf("la corrección no lleva el error: %q", msg)
	}
	if len(res.Steps) != 4 || res.Steps[1].Status != StepError || res.Steps[3].Status != StepOK || res.Steps[3].Index != 4 {
		t.Fatalf("pasos: %+v", res.Steps)
	}
	if !strings.Contains(res.Text, "Primer intento") || !strings.Contains(res.Text, "Corregido") {
		t.Errorf("texto: %q", res.Text)
	}
}

// El tope de pasos manda: con MaxSteps=2 el primer intento (2 pasos) lo agota, y
// el error se muestra sin otra vuelta al modelo.
func TestAutoSaveRespectsTheStepLimit(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("```openscad\ncube([10,10,10);\n```", nil, 10, 5),
		oaStream("no debería pedirse", nil, 1, 1),
	}}
	as := &autoSaveFake{outcome: []*AutoSaveOutcome{failOutcome("001-caja.scad", "error: algo")}}
	cfg := baseConfig(f.client(t), nil)
	cfg.AutoSave, cfg.MaxSteps = as.save, 2
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 || len(res.Steps) != 2 || res.Steps[1].Status != StepError {
		t.Errorf("pedidos %d, pasos %+v", f.count(), res.Steps)
	}
}

// Si el modelo YA creó un checkpoint con una herramienta, la red no hace nada:
// guardarlo otra vez lo duplicaría.
func TestAutoSaveSkipsATurnThatCreatedACheckpoint(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", []oaCall{{"c1", "create_checkpoint", `{"label":"caja","source":"cube(10);"}`}}, 10, 5),
		oaStream("Listo, mira:\n"+blockOK, nil, 10, 5),
	}}
	tools := newFakeTools("create_checkpoint", "render")
	tools.on("create_checkpoint", func(_ context.Context, _ map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: `{"file":"002-caja.scad"}`}, nil
	})
	as := &autoSaveFake{outcome: []*AutoSaveOutcome{okOutcome("003-dup.scad")}}
	cfg := baseConfig(f.client(t), tools)
	cfg.AutoSave = as.save
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(as.texts) != 0 {
		t.Errorf("no debía llamarse: %q", as.texts)
	}
	if len(res.Steps) != 1 || res.Steps[0].Tool != "create_checkpoint" {
		t.Errorf("pasos: %+v", res.Steps)
	}
}

// Sin bloque en el texto no hay nada que guardar; y sin AutoSave (modo «Solo
// consulta») el turno es el de siempre.
func TestAutoSaveNothingToSave(t *testing.T) {
	f := &fakeOpenAI{script: []string{oaStream("No hace falta código.", nil, 10, 5)}}
	as := &autoSaveFake{}
	cfg := baseConfig(f.client(t), nil)
	cfg.AutoSave = as.save
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil || len(res.Steps) != 0 || len(as.texts) != 1 {
		t.Fatalf("%+v %v %q", res.Steps, err, as.texts)
	}
	f2 := &fakeOpenAI{script: []string{oaStream("x\n"+blockOK, nil, 10, 5)}}
	if res, err := Run(context.Background(), baseConfig(f2.client(t), nil), Hooks{}); err != nil || len(res.Steps) != 0 {
		t.Errorf("sin AutoSave: %+v %v", res.Steps, err)
	}
}

// Lo mismo con CADA herramienta que deja un checkpoint (la lista sale de
// mcp.ToolsWithEffect, no de este archivo): la red de seguridad no guarda un
// bloque de más tras set_parameters, create_from_template, edit_mesh, etc. Y una
// herramienta que solo cambia el árbol (trash_checkpoint) no cuenta: ahí sí hay que
// guardar el bloque.
func TestAutoSaveSkipsEveryToolThatCreatesACheckpoint(t *testing.T) {
	for _, name := range mcp.ToolsWithEffect(mcp.EffectCheckpoint) {
		t.Run(name, func(t *testing.T) {
			f := &fakeOpenAI{script: []string{
				oaStream("", []oaCall{{"c1", name, `{}`}}, 10, 5),
				oaStream("Listo, mira:\n"+blockOK, nil, 10, 5),
			}}
			tools := newFakeTools(name)
			tools.on(name, func(_ context.Context, _ map[string]any) (mcp.ToolOutput, error) {
				return mcp.ToolOutput{Text: `{"file":"002-caja.scad","parent":"001-caja.scad"}`}, nil
			})
			as := &autoSaveFake{outcome: []*AutoSaveOutcome{okOutcome("003-dup.scad")}}
			cfg := baseConfig(f.client(t), tools)
			cfg.AutoSave = as.save
			res, err := Run(context.Background(), cfg, Hooks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(as.texts) != 0 {
				t.Errorf("la red de seguridad no debía llamarse: %q", as.texts)
			}
			if len(res.Steps) != 1 || res.Steps[0].Tool != name || res.Steps[0].Status != StepOK {
				t.Errorf("pasos: %+v", res.Steps)
			}
		})
	}

	// Una herramienta del árbol o de lectura no crea nada: el bloque sí se guarda.
	for _, name := range []string{"trash_checkpoint", "rename_checkpoint", "render"} {
		t.Run("no_cuenta_"+name, func(t *testing.T) {
			f := &fakeOpenAI{script: []string{
				oaStream("", []oaCall{{"c1", name, `{}`}}, 10, 5),
				oaStream("Listo, mira:\n"+blockOK, nil, 10, 5),
			}}
			as := &autoSaveFake{outcome: []*AutoSaveOutcome{okOutcome("002-caja.scad")}}
			cfg := baseConfig(f.client(t), newFakeTools(name))
			cfg.AutoSave = as.save
			if _, err := Run(context.Background(), cfg, Hooks{}); err != nil {
				t.Fatal(err)
			}
			if len(as.texts) != 1 {
				t.Errorf("la red debía guardar el bloque: %q", as.texts)
			}
		})
	}
}
