package e2e

import (
	"regexp"
	"testing"
)

const rootUsage = `Usage: patches [-hV] [COMMAND]
A command line tool for managing Git patches
  -h, --help      Show this help message and exit.
  -V, --version   Print version information and exit.
Commands:
  init             Create a new patches file if not existing and downloads the
                     base repository as specified in patches.toml, but
                     does not apply any patches.
  apply            Applies the patches to the base repository
  clean            Delete all work files
  create-patch     Creates a new patch from the current commit
  config           Changes a value in the current patches config file
  reset            Resets the workdir repository to the config base ref and the
                     remote head state
  convert-patches  Convert our patch format to git patches
`

const initUsage = `Usage: patches init [-F=<configFile>] [-U=<url>]
Create a new patches file if not existing and downloads the base repository as
specified in patches.toml, but does not apply any patches.
  -F, --config=<configFile>
                         Path to patches config file. Defaults to patches.toml
  -U, --repo-url=<url>   URL of the Base Repo
`

const configUsage = `Usage: patches config [-BRF=<baseRepoRef>] [-BRU=<baseRepoUrl>]
                      [-F=<configFile>] [-PDP=<patchesDirectoryPath>]
Changes a value in the current patches config file
      -BRF, --base-repo-ref=<baseRepoRef>
         Set the default repository ref
      -BRU, --base-repo-url=<baseRepoUrl>
         Set the base repository url
  -F, --config=<configFile>
         Path to patches config file. Defaults to patches.toml
      -PDP, --patches-directory-path=<patchesDirectoryPath>
         Set the path to the patches directory
`

func TestHelpAndVersion(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"-hV"}, {"-h", "init"}} {
		r := run(t, dir, "", args...)
		expectCode(t, r, 0)
		if r.Stdout != rootUsage {
			t.Fatalf("%v: unexpected usage:\n%s", args, r.Stdout)
		}
	}
	for _, args := range [][]string{{"-V"}, {"--version"}, {"-V", "foo"}, {"--version", "init"}} {
		r := run(t, dir, "", args...)
		expectCode(t, r, 0)
		want := "patches " + expectedVersion + "\n"
		if expectedVersion == "" {
			if !regexp.MustCompile(`^patches \S+\n$`).MatchString(r.Stdout) {
				t.Fatalf("%v: unexpected version output %q", args, r.Stdout)
			}
		} else if r.Stdout != want {
			t.Fatalf("%v: version output %q, want %q", args, r.Stdout, want)
		}
	}
}

func TestNoCommand(t *testing.T) {
	r := run(t, t.TempDir(), "")
	expectCode(t, r, 0)
	expectLogLine(t, r, "ERROR dev.taiqane.patches.cli.PatchesCLI - No command specified. Use 'patches --help' for usage information.")
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		args   []string
		stderr string
	}{
		{[]string{"foo"}, "Unmatched argument at index 0: 'foo'\n" + rootUsage},
		{[]string{"ini"}, "Unmatched argument at index 0: 'ini'\nDid you mean: patches init?\n"},
		{[]string{"applyy"}, "Unmatched argument at index 0: 'applyy'\nDid you mean: patches apply?\n"},
		{[]string{"--", "init"}, "Unmatched argument at index 1: 'init'\nDid you mean: patches init?\n"},
		{[]string{"x", "y"}, "Unmatched arguments from index 0: 'x', 'y'\n" + rootUsage},
		{[]string{"-x"}, "Unknown option: '-x'\n" + rootUsage},
		{[]string{"-F", "x"}, "Unknown options: '-F', 'x'\n" + rootUsage},
		{[]string{"--ver"}, "Unknown option: '--ver'\nPossible solutions: --version\n"},
		{[]string{"init", "--help"}, "Unknown option: '--help'\n" + initUsage},
		{[]string{"init", "-F"}, "Missing required parameter for option '--config' (<configFile>)\n" + initUsage},
		{[]string{"init", "-F", "a", "--config", "b"}, "option '--config' (<configFile>) should be specified only once\n" + initUsage},
		{[]string{"init", "-F", "-U", "x"}, "Expected parameter for option '--config' but found '-U'\n" + initUsage},
		{[]string{"init", "x"}, "Unmatched argument at index 1: 'x'\n" + initUsage},
		{[]string{"init", "x", "y"}, "Unmatched arguments from index 1: 'x', 'y'\n" + initUsage},
		{[]string{"init", "x", "--y"}, "Unmatched arguments from index 1: 'x', '--y'\n" + initUsage},
		{[]string{"init", "--x", "--y"}, "Unknown options: '--x', '--y'\n" + initUsage},
		{[]string{"init", "--", "x"}, "Unmatched argument at index 2: 'x'\n" + initUsage},
		{[]string{"init", "-"}, "Unmatched argument at index 1: '-'\n" + initUsage},
		{[]string{"init", "--conf"}, "Unknown option: '--conf'\nPossible solutions: --config\n"},
		{[]string{"config", "-BRUx"}, "Unknown option: '-BRUx'\nPossible solutions: -BRU, -BRF\n"},
		{[]string{"config", "--base"}, "Unknown option: '--base'\nPossible solutions: --base-repo-url, --base-repo-ref\n"},
		{[]string{"config", "-BRU"}, "Missing required parameter for option '--base-repo-url' (<baseRepoUrl>)\n" + configUsage},
	}
	dir := t.TempDir()
	for _, tc := range tests {
		r := run(t, dir, "", tc.args...)
		if r.Code != 2 {
			t.Errorf("%v: exit code %d, want 2 (stderr %q)", tc.args, r.Code, r.Stderr)
			continue
		}
		if r.Stderr != tc.stderr {
			t.Errorf("%v: stderr\n%q\nwant\n%q", tc.args, r.Stderr, tc.stderr)
		}
		if r.Stdout != "" {
			t.Errorf("%v: unexpected stdout %q", tc.args, r.Stdout)
		}
	}
}

func TestSubcommandUsages(t *testing.T) {
	usages := map[string]string{
		"apply": `Usage: patches apply [-F=<configFile>]
Applies the patches to the base repository
  -F, --config=<configFile>
         Path to patches config file. Defaults to patches.toml
`,
		"clean": `Usage: patches clean [-F=<configFile>]
Delete all work files
  -F, --config=<configFile>
         Path to patches config file. Defaults to patches.toml
`,
		"create-patch": `Usage: patches create-patch [-F=<configFile>] [-n=<name>]
Creates a new patch from the current commit
  -F, --config=<configFile>
                         Path to patches config file. Defaults to patches.toml
  -n, --name=<name>      Name of the patch file (without .patch). Asked for
                           on standard input if not given.
`,
		"reset": `Usage: patches reset [-F=<configFile>]
Resets the workdir repository to the config base ref and the remote head state
  -F, --config=<configFile>
         Path to patches config file. Defaults to patches.toml
`,
		"convert-patches": `Usage: patches convert-patches [-F=<configFile>]
Convert our patch format to git patches
  -F, --config=<configFile>
         Path to patches config file. Defaults to patches.toml
`,
		"config": configUsage,
		"init":   initUsage,
	}
	dir := t.TempDir()
	for cmd, usage := range usages {
		r := run(t, dir, "", cmd, "--x")
		expectCode(t, r, 2)
		if want := "Unknown option: '--x'\n" + usage; r.Stderr != want {
			t.Errorf("%s: stderr\n%s\nwant\n%s", cmd, r.Stderr, want)
		}
	}
}

func TestOptionSyntaxVariants(t *testing.T) {
	// All spellings must select the given config file, which does not exist.
	dir := t.TempDir()
	for _, args := range [][]string{
		{"clean", "-F", "nope.toml"},
		{"clean", "-Fnope.toml"},
		{"clean", "-F=nope.toml"},
		{"clean", "--config", "nope.toml"},
		{"clean", "--config=nope.toml"},
	} {
		r := run(t, dir, "", args...)
		expectCode(t, r, 8)
		expectLogLine(t, r, "ERROR d.t.p.c.PatchesConfiguration - No config file nope.toml found. Run patches init first!")
	}
}
