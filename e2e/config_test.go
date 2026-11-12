package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// propsBody strips the "#<date>" comment line written by Properties.store.
func propsBody(t *testing.T, path string) string {
	t.Helper()
	content := readFile(t, path)
	first, rest, ok := strings.Cut(content, "\n")
	if !ok || !regexp.MustCompile(`^#\w{3} \w{3} \d\d \d\d:\d\d:\d\d \S+ \d{4}$`).MatchString(first) {
		t.Fatalf("unexpected properties header in %s:\n%s", path, content)
	}
	return rest
}

func TestInitCreatesExampleConfig(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "init")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.InitCommand - Create new base patches config file! Happy working :)")
	want := "baseRepoRef=main\nbaseRepoUrl=no\\://op\npatchesDirectoryPath=patches\n"
	if got := propsBody(t, filepath.Join(dir, "patches.properties")); got != want {
		t.Fatalf("patches.properties:\n%s\nwant\n%s", got, want)
	}
	if exists(filepath.Join(dir, "patches")) || exists(filepath.Join(dir, "_workdir")) {
		t.Fatal("init with example url must not create directories")
	}

	// A second init refuses to work with the example url.
	r = run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.InitCommand - Example url no://op cannot be used to download the base repository! Aborting!")
}

func TestInitWithMissingConfigFileWritesDefaultFile(t *testing.T) {
	// The -F file does not exist, so the example config is written to
	// patches.properties (not to the -F path) with the -U url.
	f := newFixture(t)
	r := run(t, f.Project, "", "init", "-F", "custom.properties", "-U", f.Base)
	expectCode(t, r, 0)
	if exists(filepath.Join(f.Project, "custom.properties")) {
		t.Fatal("custom.properties must not be created")
	}
	body := propsBody(t, filepath.Join(f.Project, "patches.properties"))
	if !strings.Contains(body, "baseRepoRef=main\n") || !strings.Contains(body, "patchesDirectoryPath=patches\n") {
		t.Fatalf("unexpected config:\n%s", body)
	}
	if !exists(filepath.Join(f.workdir(), "a.txt")) || !exists(filepath.Join(f.Project, "patches")) {
		t.Fatal("repository was not cloned / patches dir missing")
	}
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Start to download the repository")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Successfully downloaded the repository")
}

func TestInitClonesBranch(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	if got := strings.TrimSpace(git(t, f.workdir(), "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("HEAD = %s, want main", got)
	}
	if headCommit(t, f.workdir()) != headCommit(t, f.Base) {
		t.Fatal("workdir is not at upstream main")
	}
	if statusPorcelain(t, f.workdir()) != "" {
		t.Fatal("fresh clone is not clean")
	}
	// all branches and tags are available
	git(t, f.workdir(), "rev-parse", "origin/dev", "v1.0.0", "v2.0.0")
	if got := strings.TrimSpace(git(t, f.workdir(), "config", "branch.main.merge")); got != "refs/heads/main" {
		t.Fatalf("branch.main.merge = %q", got)
	}
}

func TestInitClonesOtherBranchAndTags(t *testing.T) {
	cases := map[string]string{"dev": "dev", "v1.0.0": "v1.0.0", "v2.0.0": "v2.0.0^{commit}", "refs/heads/dev": "dev"}
	for ref, rev := range cases {
		t.Run(strings.ReplaceAll(ref, "/", "_"), func(t *testing.T) {
			f := newFixture(t)
			f.initProject(t, ref)
			want := strings.TrimSpace(git(t, f.Base, "rev-parse", rev))
			if got := headCommit(t, f.workdir()); got != want {
				t.Fatalf("HEAD = %s, want %s", got, want)
			}
			if strings.HasPrefix(ref, "v") {
				// tags are checked out detached
				if got := strings.TrimSpace(git(t, f.workdir(), "rev-parse", "--abbrev-ref", "HEAD")); got != "HEAD" {
					t.Fatalf("expected detached HEAD, got %s", got)
				}
			}
		})
	}
}

func TestInitUnknownRef(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "does-not-exist")
	r := run(t, f.Project, "", "init")
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at downloading the repository")
	if exists(f.workdir()) {
		t.Fatal("_workdir must be removed after a failed clone")
	}
}

func TestInitInvalidRemote(t *testing.T) {
	f := newFixture(t)
	r := run(t, f.Project, "", "init", "-U", filepath.Join(f.Project, "missing-repo"))
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at downloading the repository")
}

func TestInitExistingWorkdir(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	r := run(t, f.Project, "", "init")
	// JGit throws an unchecked exception that ends up in picocli -> exit 1
	expectCode(t, r, 1)
}

func TestInitInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches.properties"), "baseRepoUrl=x\n", 0o644)
	r := run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "INFO  d.t.p.c.PatchesConfiguration - Keys cannot be loaded from properties file!")
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.InitCommand - No valid patches configuration found?")

	writeFile(t, filepath.Join(dir, "patches.properties"), "# only a comment\n\n", 0o644)
	r = run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.p.c.PatchesConfiguration - Could not load properties file")
}

func TestPropertiesParsing(t *testing.T) {
	// Exercise the java.util.Properties syntax: ':' separators, whitespace,
	// escapes, comments and line continuations.
	f := newFixture(t)
	url := strings.NewReplacer(`\`, `\\`, ":", `\:`).Replace(f.Base)
	content := "! comment\n# another comment\n" +
		"  baseRepoUrl : " + url + "\n" +
		"baseRepoRef   ma\\\n      in\n" +
		"patchesDirectoryPath=my\\ patches\n"
	writeFile(t, filepath.Join(f.Project, "patches.properties"), content, 0o644)
	r := run(t, f.Project, "", "init")
	expectCode(t, r, 0)
	if !exists(filepath.Join(f.Project, "my patches")) {
		t.Fatal("patches directory 'my patches' was not created")
	}
	if got := strings.TrimSpace(git(t, f.workdir(), "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("HEAD = %s", got)
	}
}

func TestConfigCommand(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "config", "-BRU", "x")
	expectCode(t, r, 42)
	expectLogLine(t, r, "ERROR d.t.p.cli.command.ConfigCommand - No patches config file found. Please run patches init first!")

	expectCode(t, run(t, dir, "", "init"), 0)
	if err := os.Mkdir(filepath.Join(dir, "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "patches", "x.patch"), "content", 0o644)

	r = run(t, dir, "", "config", "--base-repo-url=https://example.com/repo.git", "-BRF", "v1.2.3", "-PDP", "renamed dir")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing base repo url to 'https://example.com/repo.git' from 'no://op'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing base repo ref to 'v1.2.3' from 'main'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing patches directory path to 'renamed dir' from 'patches'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Saving configuration changes to disk")
	want := "baseRepoRef=v1.2.3\nbaseRepoUrl=https\\://example.com/repo.git\npatchesDirectoryPath=renamed dir\n"
	if got := propsBody(t, filepath.Join(dir, "patches.properties")); got != want {
		t.Fatalf("patches.properties:\n%s\nwant\n%s", got, want)
	}
	if readFile(t, filepath.Join(dir, "renamed dir", "x.patch")) != "content" || exists(filepath.Join(dir, "patches")) {
		t.Fatal("patches directory was not renamed")
	}

	// Renaming to an existing directory fails, but the config is still changed.
	if err := os.Mkdir(filepath.Join(dir, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = run(t, dir, "", "config", "-PDP", "other")
	expectCode(t, r, 0)
	expectLogLine(t, r, "ERROR d.t.p.internal.file.FileService - Either the current patches directory does not exist or the new name of the directory is already in use! Aborting!")
	if !strings.Contains(propsBody(t, filepath.Join(dir, "patches.properties")), "patchesDirectoryPath=other\n") {
		t.Fatal("config not updated")
	}

	// special characters are escaped like java.util.Properties does
	r = run(t, dir, "", "config", "-BRF", " a=b:c#d!e\\f\tg h")
	expectCode(t, r, 0)
	if body := propsBody(t, filepath.Join(dir, "patches.properties")); !strings.Contains(body, "baseRepoRef=\\ a\\=b\\:c\\#d\\!e\\\\f\\tg h\n") {
		t.Fatalf("unexpected escaping:\n%s", body)
	}
}

func TestConfigWithInvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches.properties"), "foo=bar\n", 0o644)
	r := run(t, dir, "", "config", "-BRF", "x")
	expectCode(t, r, 1) // NullPointerException in Java
}
