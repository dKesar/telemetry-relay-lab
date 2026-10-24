package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/dKesar/telemetry-relay-lab/internal/research"
	"io"
	"os"
	"os/signal"
)

func researchCommand(args []string, out io.Writer) error {
	f := flag.NewFlagSet("research", flag.ContinueOnError)
	runs := f.Int("runs", 30, "independent seeds per case (2..100)")
	seed := f.Int64("seed", 1000, "held-out base seed")
	dest := f.String("out", "results/research-v3", "new evidence directory")
	render := f.String("render", "", "rebuild derived figures and summary from an existing evidence directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *render != "" {
		return research.Rebuild(*render)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return research.Generate(ctx, *dest, *runs, *seed, out)
}
