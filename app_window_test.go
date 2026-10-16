package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{sub, sub},
		{filepath.Join(sub, "no", "existe"), sub},
		{file, sub},
		{"", ""},
		{"relativa/no", ""},
	}
	for _, c := range cases {
		if got := existingDir(c.in); got != c.want {
			t.Errorf("existingDir(%q) = %q, quería %q", c.in, got, c.want)
		}
	}
}
