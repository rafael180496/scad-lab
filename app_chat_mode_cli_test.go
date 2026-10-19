//go:build !windows

package main

import (
	"strings"
	"testing"
)

// CLI (falso): sin opciones, el argv sale del modo de la conversación. Una nueva
// arranca en «Agente» (escritura habilitada); tras SetChatMode("ask") el argv es el
// de consulta (sin acceptEdits ni herramientas de escritura); otra conversación
// del mismo CLI sigue en «Agente»; y el override del envío no toca lo guardado.
func TestCLISendUsesTheConversationMode(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	c1, _ := a.NewChat(wsID, "cli:claude", "")
	c2, _ := a.NewChat(wsID, "cli:claude", "")
	if c1.Mode != "" || c2.Mode != "" {
		t.Fatalf("una conversación nueva es Agente: %+v %+v", c1, c2)
	}
	send := func(id int64, text string, opts SendOptions) {
		t.Helper()
		if _, err := a.SendChatMessage(id, text, "", nil, opts); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)
	}
	isAgent := func(args []string) bool {
		return argHas(args, "--permission-mode", "acceptEdits") && strings.Contains(argValue(args, "--allowedTools"), "mcp__scadlab__create_checkpoint")
	}
	isAsk := func(args []string) bool {
		return !argHas(args, "--permission-mode") && !strings.Contains(argValue(args, "--allowedTools"), "create_checkpoint") &&
			strings.Contains(argValue(args, "--disallowedTools"), "Edit")
	}

	send(c1.ID, "haz una caja", SendOptions{})
	if err := a.SetChatMode(c1.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	send(c1.ID, "¿cabe en la placa?", SendOptions{})
	send(c2.ID, "haz otra caja", SendOptions{})
	send(c1.ID, "créala igual", SendOptions{Agent: boolp(true)})
	send(c2.ID, "solo explica", SendOptions{Agent: boolp(false)})

	calls := f.calls("claude")
	if len(calls) != 5 {
		t.Fatalf("llamadas: %d", len(calls))
	}
	if !isAgent(calls[0]) {
		t.Errorf("conversación nueva: debía arrancar en Agente: %v", calls[0])
	}
	if !isAsk(calls[1]) || !strings.Contains(calls[1][1], "Query only") {
		t.Errorf("tras SetChatMode(ask): argv de consulta: %v", calls[1])
	}
	if !isAgent(calls[2]) {
		t.Errorf("otra conversación del mismo CLI sigue en Agente: %v", calls[2])
	}
	if !isAgent(calls[3]) {
		t.Errorf("override Agent=true: ese turno escribe: %v", calls[3])
	}
	if !isAsk(calls[4]) {
		t.Errorf("override Agent=false: ese turno consulta: %v", calls[4])
	}
	if v, _ := a.GetChat(c1.ID); v.Chat.Mode != "ask" {
		t.Errorf("el override no cambia el modo guardado: %+v", v.Chat)
	}
	if v, _ := a.GetChat(c2.ID); v.Chat.Mode != "" {
		t.Errorf("el override no cambia el modo guardado: %+v", v.Chat)
	}
}
