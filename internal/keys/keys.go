package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

const (
	devicePrivatePrefix = "TRACEHUB-ED25519-PRIVATE-KEY-1 "
	devicePublicPrefix  = "TRACEHUB-ED25519-PUBLIC-KEY-1 "
)

func GenerateServer(privatePath, publicPath string) error {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return err
	}
	if err := writeNew(privatePath, []byte(identity.String()+"\n"), 0o600); err != nil {
		return err
	}
	if err := writeNew(publicPath, []byte(identity.Recipient().String()+"\n"), 0o644); err != nil {
		_ = os.Remove(privatePath)
		return err
	}
	return nil
}

func GenerateDevice(privatePath, publicPath string) error {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	priv := devicePrivatePrefix + base64.RawStdEncoding.EncodeToString(privateKey) + "\n"
	pub := devicePublicPrefix + base64.RawStdEncoding.EncodeToString(publicKey) + "\n"
	if err := writeNew(privatePath, []byte(priv), 0o600); err != nil {
		return err
	}
	if err := writeNew(publicPath, []byte(pub), 0o644); err != nil {
		_ = os.Remove(privatePath)
		return err
	}
	return nil
}

func LoadDevicePrivate(path string) (ed25519.PrivateKey, error) {
	b, err := readPrivate(path)
	if err != nil {
		return nil, err
	}
	key, err := decodeEd25519(strings.TrimSpace(string(b)), devicePrivatePrefix, ed25519.PrivateKeySize)
	return ed25519.PrivateKey(key), err
}

func LoadDevicePublic(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := decodeEd25519(strings.TrimSpace(string(b)), devicePublicPrefix, ed25519.PublicKeySize)
	return ed25519.PublicKey(key), err
}

func LoadServerIdentity(path string) (age.Identity, error) {
	b, err := readPrivate(path)
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(strings.TrimSpace(string(b)))
}

func LoadServerRecipient(path string) (age.Recipient, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Recipient(strings.TrimSpace(string(b)))
}

func decodeEd25519(value, prefix string, size int) ([]byte, error) {
	if !strings.HasPrefix(value, prefix) {
		return nil, errors.New("invalid TraceHub Ed25519 key format")
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(b) != size {
		return nil, errors.New("invalid TraceHub Ed25519 key data")
	}
	return b, nil
}

func readPrivate(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private key %s permissions must be 0600", path)
	}
	return os.ReadFile(path)
}

func writeNew(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err = f.Write(content); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
