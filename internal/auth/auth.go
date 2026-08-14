package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const (
	HeaderDevice    = "X-TraceHub-Device"
	HeaderKeyID     = "X-TraceHub-Key-Id"
	HeaderSignature = "X-TraceHub-Signature"
	MaxRequestBody  = 66 << 20
)

func SignRequest(req *http.Request, deviceID, keyID string, privateKey ed25519.PrivateKey) error {
	body, err := readAndRestore(req, MaxRequestBody)
	if err != nil {
		return err
	}
	req.Header.Set(HeaderDevice, deviceID)
	req.Header.Set(HeaderKeyID, keyID)
	canonical := canonicalRequest(req, body)
	req.Header.Set(HeaderSignature, base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)))
	return nil
}

func VerifyRequest(req *http.Request, publicKey ed25519.PublicKey) error {
	body, err := readAndRestore(req, MaxRequestBody)
	if err != nil {
		return err
	}
	sig, err := base64.RawStdEncoding.DecodeString(req.Header.Get(HeaderSignature))
	if err != nil {
		return fmt.Errorf("invalid signature encoding")
	}
	if !ed25519.Verify(publicKey, canonicalRequest(req, body), sig) {
		return fmt.Errorf("invalid request signature")
	}
	return nil
}

func canonicalRequest(req *http.Request, body []byte) []byte {
	names := make([]string, 0)
	for name := range req.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-tracehub-") && !strings.EqualFold(name, HeaderSignature) {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("TRACEHUB-V1\n")
	b.WriteString(strings.ToUpper(req.Method))
	b.WriteByte('\n')
	b.WriteString(req.URL.RequestURI())
	b.WriteByte('\n')
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(req.Header.Get(name)))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256(body)
	b.WriteString("body-sha256:")
	b.WriteString(hex.EncodeToString(sum[:]))
	b.WriteByte('\n')
	return []byte(b.String())
}

func readAndRestore(req *http.Request, max int64) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(req.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("request body exceeds %d bytes", max)
	}
	req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}
