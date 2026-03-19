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
	worker := app.BuildWorkerApp(cfg)
	defer worker.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := worker.Run(ctx); err != nil {
		log.Fatalf("worker stopped with error: %v", err)
	}
}
