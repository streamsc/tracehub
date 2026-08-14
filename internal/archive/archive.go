package archive

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"filippo.io/age"
)

func Encrypt(plain []byte, recipient age.Recipient) ([]byte, error) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	var encrypted bytes.Buffer
	aw, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := aw.Write(compressed.Bytes()); err != nil {
		return nil, err
	}
	if err := aw.Close(); err != nil {
		return nil, err
	}
	return encrypted.Bytes(), nil
}

func Decrypt(ciphertext []byte, identity age.Identity, maxPlain int64) ([]byte, error) {
	ar, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(ar)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	plain, err := io.ReadAll(io.LimitReader(zr, maxPlain+1))
	if err != nil {
		return nil, err
	}
	if int64(len(plain)) > maxPlain {
		return nil, fmt.Errorf("decompressed chunk exceeds %d bytes", maxPlain)
	}
	return plain, nil
}
