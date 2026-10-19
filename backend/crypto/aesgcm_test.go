package crypto

import (
	"bytes"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, 32) }

func TestEncryptDecryptRoundTrip(t *testing.T) {
	ct, nonce, err := Encrypt(testKey(), []byte("sk-secreto"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("sk-secreto")) {
		t.Fatal("el texto cifrado contiene la clave en claro")
	}
	got, err := Decrypt(testKey(), ct, nonce)
	if err != nil || string(got) != "sk-secreto" {
		t.Fatalf("round trip: %q, %v", got, err)
	}
	// Dos cifrados del mismo texto no coinciden: el nonce es aleatorio.
	ct2, nonce2, _ := Encrypt(testKey(), []byte("sk-secreto"))
	if bytes.Equal(ct, ct2) || bytes.Equal(nonce, nonce2) {
		t.Error("el nonce se repitió")
	}
}

func TestDecryptRejectsWrongKeyTamperedAndBadNonce(t *testing.T) {
	ct, nonce, _ := Encrypt(testKey(), []byte("x"))
	if _, err := Decrypt(bytes.Repeat([]byte{8}, 32), ct, nonce); err == nil {
		t.Error("otra clave no debería abrir")
	}
	ct[0] ^= 1
	if _, err := Decrypt(testKey(), ct, nonce); err == nil {
		t.Error("un texto alterado no debería abrir")
	}
	if _, err := Decrypt(testKey(), ct, nonce[:3]); err == nil {
		t.Error("un nonce de largo incorrecto no debería abrir")
	}
	if _, _, err := Encrypt([]byte("corta"), []byte("x")); err == nil {
		t.Error("una clave que no es de 32 bytes no sirve")
	}
}
