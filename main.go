// Command patches manages a set of patches on top of a git repository.
// It is a Go port of https://github.com/dotMatthew/patches with the same
// command line interface and output.
package main

import "os"

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr))
}
