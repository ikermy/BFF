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
	application := app.BuildAPIApp(cfg)
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		log.Fatalf("api stopped with error: %v", err)
	}
}
