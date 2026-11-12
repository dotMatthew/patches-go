package e2e

import (
	"fmt"
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

// tomlBody reads patches.toml and checks its header comment.
func tomlBody(t *testing.T, path string) string {
	t.Helper()
	content := readFile(t, path)
	rest, ok := strings.CutPrefix(content, "# patches configuration\n")
	if !ok {
		t.Fatalf("unexpected header in %s:\n%s", path, content)
	}
	return rest
}

// expectConfig checks the saved config: java is the expected
// patches.properties body, toml the expected patches.toml body.
func expectConfig(t *testing.T, dir, java, toml string) {
	t.Helper()
	if javaImpl {
		if got := propsBody(t, filepath.Join(dir, "patches.properties")); got != java {
			t.Fatalf("patches.properties:\n%s\nwant\n%s", got, java)
		}
		return
	}
	if got := tomlBody(t, filepath.Join(dir, "patches.toml")); got != toml {
		t.Fatalf("patches.toml:\n%s\nwant\n%s", got, toml)
	}
	if exists(filepath.Join(dir, "patches.properties")) {
		t.Fatal("the Go version must not write patches.properties")
	}
}

func TestInitCreatesExampleConfig(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "init")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.InitCommand - Create new base patches config file! Happy working :)")
	expectConfig(t, dir,
		"baseRepoRef=main\nbaseRepoUrl=no\\://op\npatchesDirectoryPath=patches\n",
		"base_repo_url = \"no://op\"\nbase_repo_ref = \"main\"\npatches_directory = \"patches\"\n")
	if exists(filepath.Join(dir, "patches")) || exists(filepath.Join(dir, "_workdir")) {
		t.Fatal("init with example url must not create directories")
	}

	// A second init refuses to work with the example url.
	r = run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.InitCommand - Example url no://op cannot be used to download the base repository! Aborting!")
}

func TestInitWithMissingConfigFile(t *testing.T) {
	// The -F file does not exist: init creates the example config with the -U
	// url. The Java version writes it to patches.properties (ignoring -F),
	// the Go version to the -F file (as TOML).
	f := newFixture(t)
	r := run(t, f.Project, "", "init", "-F", "custom.properties", "-U", f.Base)
	expectCode(t, r, 0)
	created, notCreated := "custom.toml", []string{"custom.properties", "patches.toml", "patches.properties"}
	if javaImpl {
		created, notCreated = "patches.properties", []string{"custom.properties", "custom.toml"}
	}
	for _, n := range notCreated {
		if exists(filepath.Join(f.Project, n)) {
			t.Fatalf("%s must not be created", n)
		}
	}
	cfg := readFile(t, filepath.Join(f.Project, created))
	for _, want := range map[bool][]string{
		true:  {"baseRepoRef=main\n", "patchesDirectoryPath=patches\n"},
		false: {"base_repo_ref = \"main\"\n", "patches_directory = \"patches\"\n"},
	}[javaImpl] {
		if !strings.Contains(cfg, want) {
			t.Fatalf("unexpected config:\n%s", cfg)
		}
	}
	if !exists(filepath.Join(f.workdir(), "a.txt")) || !exists(filepath.Join(f.Project, "patches")) {
		t.Fatal("repository was not cloned / patches dir missing")
	}
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Start to download the repository")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Successfully downloaded the repository")
}

func TestInitExampleConfigWithExplicitFile(t *testing.T) {
	goOnly(t)
	dir := t.TempDir()
	r := run(t, dir, "", "init", "-F", "my.toml")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.InitCommand - Create new base patches config file! Happy working :)")
	if !exists(filepath.Join(dir, "my.toml")) || exists(filepath.Join(dir, "patches.toml")) {
		t.Fatal("the example config must be written to the -F file")
	}
	// the other commands use the same file
	r = run(t, dir, "", "config", "-F", "my.toml", "-BRF", "dev")
	expectCode(t, r, 0)
	if !strings.Contains(readFile(t, filepath.Join(dir, "my.toml")), "base_repo_ref = \"dev\"\n") {
		t.Fatal("my.toml was not updated")
	}
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
	expectConfig(t, dir,
		"baseRepoRef=v1.2.3\nbaseRepoUrl=https\\://example.com/repo.git\npatchesDirectoryPath=renamed dir\n",
		"base_repo_url = \"https://example.com/repo.git\"\nbase_repo_ref = \"v1.2.3\"\npatches_directory = \"renamed dir\"\n")
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
	if cfg := readFile(t, filepath.Join(dir, configName())); !strings.Contains(cfg, "patchesDirectoryPath=other\n") &&
		!strings.Contains(cfg, "patches_directory = \"other\"\n") {
		t.Fatalf("config not updated:\n%s", cfg)
	}

	// special characters are escaped (java.util.Properties or TOML rules)
	r = run(t, dir, "", "config", "-BRF", " a=b:c#d!e\\f\tg h")
	expectCode(t, r, 0)
	want := `base_repo_ref = " a=b:c#d!e\\f\tg h"` + "\n"
	if javaImpl {
		want = "baseRepoRef=\\ a\\=b\\:c\\#d\\!e\\\\f\\tg h\n"
	}
	if cfg := readFile(t, filepath.Join(dir, configName())); !strings.Contains(cfg, want) {
		t.Fatalf("unexpected escaping:\n%s", cfg)
	}
}

func TestConfigWithInvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches.properties"), "foo=bar\n", 0o644)
	r := run(t, dir, "", "config", "-BRF", "x")
	expectCode(t, r, 1) // NullPointerException in Java
}

func TestConfigMigration(t *testing.T) {
	goOnly(t)
	f := newFixture(t)
	f.writePropertiesConfig(t, "main")
	old := filepath.Join(f.Project, "patches.properties")

	// any command migrates patches.properties to patches.toml
	r := run(t, f.Project, "", "init")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.c.PatchesConfiguration - Migrated patches.properties to patches.toml (the old file was renamed to patches.properties.bak)")
	if exists(old) || !exists(old+".bak") {
		t.Fatal("patches.properties must be renamed to patches.properties.bak")
	}
	want := fmt.Sprintf("base_repo_url = %q\nbase_repo_ref = \"main\"\npatches_directory = \"patches\"\n", f.Base)
	if got := tomlBody(t, filepath.Join(f.Project, "patches.toml")); got != want {
		t.Fatalf("patches.toml:\n%s\nwant\n%s", got, want)
	}
	if !exists(filepath.Join(f.workdir(), "a.txt")) {
		t.Fatal("init did not clone with the migrated config")
	}

	// later commands use patches.toml; changes are only written there
	r = run(t, f.Project, "", "config", "-BRF", "dev")
	expectCode(t, r, 0)
	if strings.Contains(r.Stdout, "Migrated") || exists(old) {
		t.Fatal("the config must not be migrated twice")
	}
	if !strings.Contains(readFile(t, filepath.Join(f.Project, "patches.toml")), "base_repo_ref = \"dev\"\n") {
		t.Fatal("patches.toml was not updated")
	}
}

func TestConfigMigrationWithExplicitFile(t *testing.T) {
	goOnly(t)
	f := newFixture(t)
	f.writePropertiesConfig(t, "main")
	if err := os.Rename(filepath.Join(f.Project, "patches.properties"), filepath.Join(f.Project, "custom.properties")); err != nil {
		t.Fatal(err)
	}
	// -F with the old file name migrates to custom.toml and keeps using it
	r := run(t, f.Project, "", "config", "-F", "custom.properties", "-BRF", "dev")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.c.PatchesConfiguration - Migrated custom.properties to custom.toml (the old file was renamed to custom.properties.bak)")
	if !strings.Contains(readFile(t, filepath.Join(f.Project, "custom.toml")), "base_repo_ref = \"dev\"\n") {
		t.Fatal("custom.toml was not written")
	}
	r = run(t, f.Project, "", "config", "-F", "custom.toml", "-BRF", "main")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing base repo ref to 'main' from 'dev'")
}

func TestInvalidTOMLConfig(t *testing.T) {
	goOnly(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches.toml"), "base_repo_url = \"x\"\n", 0o644)
	r := run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "INFO  d.t.p.c.PatchesConfiguration - Keys cannot be loaded from config file!")

	writeFile(t, filepath.Join(dir, "patches.toml"), "not toml", 0o644)
	r = run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLog(t, r, "ERROR", "Failed to load patches configuration")
}
