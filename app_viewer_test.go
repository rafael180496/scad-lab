package main

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestDecodePNGDataURL(t *testing.T) {
	png := append(append([]byte{}, pngMagic...), 0, 0, 0, 13, 'I', 'H', 'D', 'R')
	good := pngDataPrefix + base64.StdEncoding.EncodeToString(png)
	got, err := decodePNGDataURL(good)
	if err != nil || !bytes.Equal(got, png) {
		t.Fatalf("PNG válido: %v, %v", got, err)
	}
	bad := []string{
		"",
		"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(png),
		"data:image/png,raw",
		pngDataPrefix + "%%%no-base64%%%",
		pngDataPrefix + base64.StdEncoding.EncodeToString([]byte("GIF89a no es png")),
	}
	for _, s := range bad {
		if _, err := decodePNGDataURL(s); err == nil {
			t.Errorf("aceptó %q", s)
		}
	}
}

func TestScreenshotName(t *testing.T) {
	cases := map[string]string{
		"006-two-color-logo-iso": "006-two-color-logo-iso.png",
		"pieza.png":              "pieza.png",
		"../../etc/passwd":       "passwd.png",
		`C:\Users\x\caja.png`:    "caja.png",
		"a:b*c?":                 "a-b-c-.png",
		"":                       "scadlab.png",
		"  ":                     "scadlab.png",
	}
	for in, want := range cases {
		if got := screenshotName(in); got != want {
			t.Errorf("screenshotName(%q) = %q, want %q", in, got, want)
		}
	}
}
