package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"

	"tracehub/internal/archive"
	"tracehub/internal/codex"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	"tracehub/internal/store"
)

func Export(ctx context.Context, cfg config.Server, deviceID, sessionID, outputPath string) error {
	db, identities, err := open(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	chunks, err := db.Chunks(ctx, deviceID, codex.AgentType, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".tracehub-export-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	var expected int64
	for _, chunk := range chunks {
		if chunk.StartOffset != expected {
			temp.Close()
			return fmt.Errorf("archive offset gap: expected %d, got %d", expected, chunk.StartOffset)
		}
		identity, ok := identities[chunk.ServerKeyID]
		if !ok {
			temp.Close()
			return fmt.Errorf("missing server key %s", chunk.ServerKeyID)
		}
		plain, err := archive.Decrypt(chunk.Ciphertext, identity, codex.MaxLine+codex.TargetChunk)
		if err != nil {
			temp.Close()
			return err
		}
		hash := sha256.Sum256(plain)
		if int64(len(plain)) != chunk.PlainSize || hex.EncodeToString(hash[:]) != chunk.PlainSHA256 {
			temp.Close()
			return fmt.Errorf("archive integrity check failed at offset %d", chunk.StartOffset)
		}
		if _, err := temp.Write(plain); err != nil {
			temp.Close()
			return err
		}
		expected = chunk.EndOffset
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempName, outputPath); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("output already exists: %s", outputPath)
		}
		return err
	}
	return os.Remove(tempName)
}

func Delete(ctx context.Context, cfg config.Server, deviceID, sessionID string) error {
	db, _, err := open(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.DeleteSession(ctx, deviceID, codex.AgentType, sessionID)
}

func open(cfg config.Server) (*store.Store, map[string]age.Identity, error) {
	db, err := store.Open(cfg.Database)
	if err != nil {
		return nil, nil, err
	}
	identities := make(map[string]age.Identity, len(cfg.ServerPrivateKeys))
	for id, path := range cfg.ServerPrivateKeys {
		identity, err := keys.LoadServerIdentity(path)
		if err != nil {
			db.Close()
			return nil, nil, err
		}
		identities[id] = identity
	}
	return db, identities, nil
}
