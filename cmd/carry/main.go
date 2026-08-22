package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ApexReasoning/carry/internal/cli"
	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/codex"
	"github.com/ApexReasoning/carry/internal/host/pi"
)

var version = "development"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, input io.Reader, output io.Writer, errorOutput io.Writer) int {
	adapters, err := hostdomain.NewAdapterSet(pi.New(), codex.New())
	if err != nil {
		_, _ = io.WriteString(errorOutput, "Error: compose native Host adapters: "+err.Error()+"\n")
		return 1
	}
	return cli.Run(
		ctx,
		arguments,
		version,
		cli.ConfigDirectory(),
		cli.Streams{Input: input, Output: output, ErrorOutput: errorOutput},
		adapters,
	)
}
