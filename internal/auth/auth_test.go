package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"testing"
)

func TestSignVerifyAndTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, "https://tracehub.test/v1/chunk?x=1", bytes.NewReader([]byte("ciphertext")))
	req.Header.Set("X-TraceHub-End-Offset", "10")
	if err := SignRequest(req, "server-a", "device-1", privateKey); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest(req, publicKey); err != nil {
		t.Fatal(err)
	}
	req.URL.RawQuery = "x=2"
	if err := VerifyRequest(req, publicKey); err == nil {
		t.Fatal("changed request path was accepted")
	}
}

func TestSignatureCoversMetadataAndBody(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	req, _ := http.NewRequest(http.MethodPost, "https://tracehub.test/v1/sync/plan", bytes.NewReader([]byte("body")))
	if err := SignRequest(req, "desktop", "key-1", privateKey); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-TraceHub-Device", "server-a")
	if err := VerifyRequest(req, publicKey); err == nil {
		t.Fatal("changed device ID was accepted")
	}
}
