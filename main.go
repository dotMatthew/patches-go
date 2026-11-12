// Command patches manages a set of patches on top of a git repository.
// It is a Go port of https://github.com/dotMatthew/patches with the same
// command line interface and output.
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	a := &app{ctx: ctx, dir: ".", stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	code := a.run(os.Args[1:])
	stop()
	os.Exit(code)
}
