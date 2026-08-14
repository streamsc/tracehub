package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"syscall"

	"tracehub/internal/admin"
	"tracehub/internal/client"
	"tracehub/internal/codex"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	"tracehub/internal/mcpproxy"
	"tracehub/internal/server"
	"tracehub/internal/syncer"
	"tracehub/internal/version"
)

func main() {
	syscall.Umask(0o077)
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tracehub:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "version":
		fmt.Println(version.Version)
		return nil
	case "keygen":
		return runKeygen(args[1:])
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		configPath := flags.String("config", "", "server JSON config")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := loadServer(*configPath)
		if err != nil {
			return err
		}
		service, err := server.New(cfg)
		if err != nil {
			return err
		}
		defer service.Close()
		fmt.Fprintf(os.Stderr, "tracehub serve listening on %s\n", cfg.Listen)
		err = service.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case "sync":
		flags := flag.NewFlagSet("sync", flag.ContinueOnError)
		configPath := flags.String("config", "", "client JSON config")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, remote, err := loadClient(*configPath)
		if err != nil {
			return err
		}
		result, err := syncer.Run(ctx, cfg.CodexDir, remote, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Printf("complete: %d session(s), %d chunk(s), %d plaintext byte(s)\n", result.Sessions, result.Chunks, result.Bytes)
		return nil
	case "mcp":
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		configPath := flags.String("config", "", "client JSON config")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		_, remote, err := loadClient(*configPath)
		if err != nil {
			return err
		}
		return mcpproxy.Run(ctx, remote)
	case "admin":
		return runAdmin(ctx, args[1:])
	default:
		return usageError()
	}
}

func runKeygen(args []string) error {
	if len(args) == 0 || (args[0] != "server" && args[0] != "device") {
		return errors.New("usage: tracehub keygen <server|device> --private PATH --public PATH")
	}
	flags := flag.NewFlagSet("keygen "+args[0], flag.ContinueOnError)
	privatePath := flags.String("private", "", "private key output path")
	publicPath := flags.String("public", "", "public key output path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *privatePath == "" || *publicPath == "" {
		return errors.New("private and public output paths are required")
	}
	if args[0] == "server" {
		return keys.GenerateServer(*privatePath, *publicPath)
	}
	return keys.GenerateDevice(*privatePath, *publicPath)
}

func runAdmin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tracehub admin <export-session|delete-session> [options]")
	}
	flags := flag.NewFlagSet("admin "+args[0], flag.ContinueOnError)
	configPath := flags.String("config", "", "server JSON config")
	deviceID := flags.String("device", "", "device ID")
	sessionID := flags.String("session", "", "session UUID")
	output := flags.String("output", "", "export output JSONL path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *deviceID == "" || !codex.SafeSessionID(*sessionID) {
		return errors.New("device and valid session UUID are required")
	}
	cfg, err := loadServer(*configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "export-session":
		if *output == "" {
			return errors.New("output path is required")
		}
		return admin.Export(ctx, cfg, *deviceID, *sessionID, *output)
	case "delete-session":
		return admin.Delete(ctx, cfg, *deviceID, *sessionID)
	default:
		return errors.New("usage: tracehub admin <export-session|delete-session> [options]")
	}
}

func loadClient(path string) (config.Client, *client.Client, error) {
	if path == "" {
		return config.Client{}, nil, errors.New("config path is required")
	}
	cfg, err := config.LoadClient(path)
	if err != nil {
		return config.Client{}, nil, err
	}
	remote, err := client.New(cfg)
	return cfg, remote, err
}

func loadServer(path string) (config.Server, error) {
	if path == "" {
		return config.Server{}, errors.New("config path is required")
	}
	return config.LoadServer(path)
}

func usageError() error {
	return errors.New("usage: tracehub <version|keygen|serve|sync|mcp|admin>")
}
