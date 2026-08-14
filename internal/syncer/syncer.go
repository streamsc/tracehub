package syncer

import (
	"context"
	"fmt"
	"io"

	"tracehub/internal/api"
	"tracehub/internal/archive"
	"tracehub/internal/client"
	"tracehub/internal/codex"
)

type Result struct {
	Sessions int
	Chunks   int
	Bytes    int64
}

func Run(ctx context.Context, codexDir string, remote *client.Client, output io.Writer) (Result, error) {
	sources, err := codex.Discover(codexDir)
	if err != nil {
		return Result{}, err
	}
	manifest := make([]api.LocalSession, 0, len(sources))
	for _, source := range sources {
		manifest = append(manifest, api.LocalSession{SessionID: source.SessionID, Size: source.Size})
	}
	offsets, err := remote.Plan(ctx, manifest)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, source := range sources {
		start, ok := offsets[source.SessionID]
		if !ok {
			return result, fmt.Errorf("server omitted offset for session %s", source.SessionID)
		}
		before := result.Chunks
		_, err := codex.ReadChunks(source.Path, start, func(chunkStart, chunkEnd int64, plain []byte) error {
			ciphertext, err := archive.Encrypt(plain, remote.Recipient())
			if err != nil {
				return err
			}
			response, err := remote.Upload(ctx, source.SessionID, chunkStart, chunkEnd, client.PlainSHA256(plain), int64(len(plain)), ciphertext)
			if err != nil {
				return err
			}
			if response.NextOffset != chunkEnd {
				return fmt.Errorf("server returned unexpected next offset %d", response.NextOffset)
			}
			result.Chunks++
			result.Bytes += int64(len(plain))
			return nil
		})
		if err != nil {
			return result, fmt.Errorf("sync %s: %w", source.SessionID, err)
		}
		if result.Chunks > before {
			result.Sessions++
			fmt.Fprintf(output, "synced %s: %d chunk(s)\n", source.SessionID, result.Chunks-before)
		}
	}
	return result, nil
}
