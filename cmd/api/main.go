package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ikermy/BFF/internal/app"
	"github.com/ikermy/BFF/internal/config"
)

func main() {
	cfg := config.Load()
	// create top-level context to propagate shutdown to stores
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application := app.BuildAPIAppWithContext(ctx, cfg)
	defer application.Close()

	if err := application.Run(ctx); err != nil {
		log.Fatalf("api stopped with error: %v", err)
	}
}
