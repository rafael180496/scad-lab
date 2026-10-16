package mcp

import (
	"context"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"scadlab/assets"
	"scadlab/backend/i18n"
)

// Plantillas de la skill openscad-templates, embebidas en el binario.
const templatesDir = "skills/openscad-templates/templates"

var templateDescRe = regexp.MustCompile(`desc="((?:[^"\\]|\\.)*)"`)

type templateOut struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// templateNames devuelve los nombres (sin .scad) ordenados.
func templateNames() ([]string, error) {
	ents, err := fs.ReadDir(assets.FS, templatesDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".scad") {
			out = append(out, strings.TrimSuffix(e.Name(), ".scad"))
		}
	}
	sort.Strings(out)
	return out, nil
}

func readTemplate(name string) (string, error) {
	raw, err := fs.ReadFile(assets.FS, path.Join(templatesDir, name+".scad"))
	return string(raw), err
}

func (e *env) listTemplates(_ context.Context, _ map[string]any) (*toolResult, error) {
	e.settings()
	names, err := templateNames()
	if err != nil {
		return nil, err
	}
	out := make([]templateOut, 0, len(names))
	for _, n := range names {
		src, err := readTemplate(n)
		if err != nil {
			continue
		}
		first, _, _ := strings.Cut(src, "\n")
		desc := ""
		if m := templateDescRe.FindStringSubmatch(first); m != nil {
			desc = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[1])
		}
		out = append(out, templateOut{Name: n, Description: desc})
	}
	return jsonResult(map[string]any{"templates": out})
}

func (e *env) getTemplate(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	name := strings.TrimSuffix(strings.TrimSpace(strArg(args, "name")), ".scad")
	// Se busca en la lista y no se arma una ruta con lo que llega: un nombre con
	// «..» o barras no puede salirse de la carpeta de plantillas.
	names, err := templateNames()
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if n == name {
			src, err := readTemplate(n)
			if err != nil {
				return nil, err
			}
			return textResult(src), nil
		}
	}
	return nil, i18n.Errorf(i18n.Msg{
		ES: "plantilla desconocida: %q (disponibles: %s)",
		EN: "unknown template: %q (available: %s)",
	}, name, strings.Join(names, ", "))
}
