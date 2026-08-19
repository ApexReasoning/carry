package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ApexReasoning/carry/internal/agent/codex"
	"github.com/ApexReasoning/carry/internal/agent/pi"
	"github.com/ApexReasoning/carry/internal/cli"
)

var version = "development"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, input io.Reader, output io.Writer, errorOutput io.Writer) int {
	return cli.Run(
		ctx,
		arguments,
		version,
		cli.ConfigDirectory(),
		cli.Streams{Input: input, Output: output, ErrorOutput: errorOutput},
		pi.New(),
		codex.New(),
	)
}
