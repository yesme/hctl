package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yesme/hctl/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit((&app.App{}).Run(ctx, os.Args[1:]))
}
