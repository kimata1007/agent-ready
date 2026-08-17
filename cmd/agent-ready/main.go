package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kimata1007/agent-ready/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Context generation may invoke a local model CLI. The first interrupt
	// cancels the context so the child can be signalled and reaped cleanly.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupts
		cancel()
		// Restoring the default behaviour means a second interrupt always stops
		// the process, even if something is not watching the context.
		signal.Reset(os.Interrupt, syscall.SIGTERM)
	}()

	application := app.New(os.Stdin, os.Stdout, os.Stderr)
	application.Version = app.Version{
		Version: version,
		Commit:  commit,
		Date:    date,
	}
	os.Exit(application.Run(ctx, os.Args[1:]))
}
