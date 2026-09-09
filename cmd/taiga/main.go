package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/KoukeNeko/taiga-cli/internal/cli"
)

func main() {
	app, err := cli.New()
	if err != nil {
		_, _ = os.Stderr.WriteString("Error: " + err.Error() + "\n")
		os.Exit(cli.ExitGeneric)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(app.Execute(ctx, os.Args[1:]))
}
