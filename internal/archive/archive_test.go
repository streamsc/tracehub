package archive

import (
	"bytes"
	"testing"

	"filippo.io/age"
)

func TestEncryptDecryptAndTamper(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("tracehub jsonl\n"), 1000)
	ciphertext, err := Encrypt(plain, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(ciphertext, identity, int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("round trip changed plaintext")
	}
	ciphertext[len(ciphertext)/2] ^= 1
	if _, err := Decrypt(ciphertext, identity, int64(len(plain))); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestDecryptLimit(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()
	ciphertext, err := Encrypt(bytes.Repeat([]byte("x"), 1024), identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(ciphertext, identity, 100); err == nil {
		t.Fatal("oversized plaintext was accepted")
	}
}

func TestWrongIdentity(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()
	wrongIdentity, _ := age.GenerateX25519Identity()
	ciphertext, err := Encrypt([]byte("secret\n"), identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(ciphertext, wrongIdentity, 1024); err == nil {
		t.Fatal("wrong identity decrypted archive")
	}
}
