package cli

import (
	"bytes"
	"strings"
	"testing"
)

func newApp() (*App, *bytes.Buffer, *bytes.Buffer, *Values) {
	var stdout, stderr bytes.Buffer
	var got Values
	run := func(v Values) int { got = v; return 7 }
	app := &App{
		Name: "tool", Usage: "root usage\n", Version: "1.2.3",
		Commands: []*Command{
			{Name: "init", Usage: "init usage\n", Run: run, Options: []Option{
				{"-F", "--config", "<file>"}, {"-U", "--url", "<url>"}}},
			{Name: "config", Usage: "config usage\n", Run: run, Options: []Option{
				{"-BRU", "--base-url", "<url>"}, {"-BRF", "--base-ref", "<ref>"}}},
		},
		Root:   func() int { return 3 },
		Stdout: &stdout, Stderr: &stderr,
	}
	return app, &stdout, &stderr, &got
}

func TestOptionSpellings(t *testing.T) {
	for _, args := range [][]string{
		{"init", "-F", "x"}, {"init", "-Fx"}, {"init", "-F=x"},
		{"init", "--config", "x"}, {"init", "--config=x"},
	} {
		app, _, stderr, got := newApp()
		if code := app.Execute(args); code != 7 || stderr.Len() != 0 {
			t.Fatalf("%v: code %d, stderr %q", args, code, stderr)
		}
		if got.Get("--config", "") != "x" || got.Has("--url") {
			t.Fatalf("%v: values %v", args, *got)
		}
	}
}

func TestRootAndFlags(t *testing.T) {
	tests := []struct {
		args   []string
		code   int
		stdout string
	}{
		{nil, 3, ""},
		{[]string{"-h"}, 0, "root usage\n"},
		{[]string{"-hV"}, 0, "root usage\n"},
		{[]string{"--version", "whatever"}, 0, "tool 1.2.3\n"},
	}
	for _, tc := range tests {
		app, stdout, _, _ := newApp()
		if code := app.Execute(tc.args); code != tc.code || stdout.String() != tc.stdout {
			t.Errorf("%v: code %d stdout %q, want %d %q", tc.args, code, stdout, tc.code, tc.stdout)
		}
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		args   []string
		stderr string
	}{
		{[]string{"x"}, "Unmatched argument at index 0: 'x'\nroot usage\n"},
		{[]string{"ini"}, "Unmatched argument at index 0: 'ini'\nDid you mean: tool init?\n"},
		{[]string{"-x", "y"}, "Unknown options: '-x', 'y'\nroot usage\n"},
		{[]string{"init", "-F"}, "Missing required parameter for option '--config' (<file>)\ninit usage\n"},
		{[]string{"init", "-F", "-U"}, "Expected parameter for option '--config' but found '-U'\ninit usage\n"},
		{[]string{"init", "-Fa", "-Fb"}, "option '--config' (<file>) should be specified only once\ninit usage\n"},
		{[]string{"init", "--", "a", "b"}, "Unmatched arguments from index 2: 'a', 'b'\ninit usage\n"},
		{[]string{"init", "--conf"}, "Unknown option: '--conf'\nPossible solutions: --config\n"},
		{[]string{"config", "-BR"}, "Unknown option: '-BR'\nPossible solutions: -BRU, -BRF\n"},
	}
	for _, tc := range tests {
		app, stdout, stderr, _ := newApp()
		if code := app.Execute(tc.args); code != 2 {
			t.Errorf("%v: code %d", tc.args, code)
		}
		if stderr.String() != tc.stderr || stdout.Len() != 0 {
			t.Errorf("%v: stderr\n%q\nwant\n%q", tc.args, stderr, tc.stderr)
		}
	}
}

func TestSimilarCommands(t *testing.T) {
	app, _, _, _ := newApp()
	if got := strings.Join(app.similarCommands("conifg"), ","); got != "config,init" {
		t.Fatalf("similarCommands = %q", got)
	}
	if got := app.similarCommands("x"); len(got) != 0 {
		t.Fatalf("single character must not match: %v", got)
	}
}
