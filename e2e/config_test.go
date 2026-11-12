package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tomlBody reads a config file and checks its header comment.
func tomlBody(t *testing.T, path string) string {
	t.Helper()
	content := readFile(t, path)
	rest, ok := strings.CutPrefix(content, "# patches configuration\n")
	if !ok {
		t.Fatalf("unexpected header in %s:\n%s", path, content)
	}
	return rest
}

func TestInitCreatesExampleConfig(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "init")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.InitCommand - Created the config file patches.toml. Set base_repo_url (or use -U) and run patches init again.")
	want := "base_repo_url = \"no://op\"\nbase_repo_ref = \"main\"\npatches_directory = \"patches\"\n"
	if got := tomlBody(t, filepath.Join(dir, configName)); got != want {
		t.Fatalf("patches.toml:\n%s\nwant\n%s", got, want)
	}
	if exists(filepath.Join(dir, "patches")) || exists(filepath.Join(dir, "_workdir")) {
		t.Fatal("init with example url must not create directories")
	}

	// A second init refuses to work with the example url.
	r = run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.InitCommand - The example url no://op in patches.toml cannot be used to download the base repository. Set base_repo_url first!")
}

func TestInitWithURLCreatesConfigAndClones(t *testing.T) {
	// The -F file does not exist: init creates it with the -U url and clones.
	// "-F name.properties" is written as name.toml.
	f := newFixture(t)
	r := run(t, f.Project, "", "init", "-F", "custom.properties", "-U", f.Base)
	expectCode(t, r, 0)
	for _, n := range []string{"custom.properties", "patches.toml"} {
		if exists(filepath.Join(f.Project, n)) {
			t.Fatalf("%s must not be created", n)
		}
	}
	want := fmt.Sprintf("base_repo_url = %q\nbase_repo_ref = \"main\"\npatches_directory = \"patches\"\n", f.Base)
	if got := tomlBody(t, filepath.Join(f.Project, "custom.toml")); got != want {
		t.Fatalf("custom.toml:\n%s\nwant\n%s", got, want)
	}
	if !exists(filepath.Join(f.workdir(), "a.txt")) || !exists(filepath.Join(f.Project, "patches")) {
		t.Fatal("repository was not cloned / patches dir missing")
	}
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Start to download the repository")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Successfully downloaded the repository")
}

func TestInitExampleConfigWithExplicitFile(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "init", "-F", "my.toml")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.InitCommand - Created the config file my.toml. Set base_repo_url (or use -U) and run patches init again.")
	if !exists(filepath.Join(dir, "my.toml")) || exists(filepath.Join(dir, configName)) {
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
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - The work directory _workdir already exists. Use patches reset to update it or patches clean to remove it!")
}

func TestInitPatchesDirIsAFile(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "main")
	writeFile(t, filepath.Join(f.Project, "patches"), "not a directory", 0o644)
	r := run(t, f.Project, "", "init")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.InitCommand - The patches directory patches exists but is not a directory!")
}

func TestInvalidConfig(t *testing.T) {
	cases := map[string]string{
		"missing key":  "base_repo_url = \"x\"\n",
		"unknown key":  "base_repo_url = \"x\"\nbase_repo_ref = \"main\"\ntypo = 1\n",
		"invalid toml": "not toml",
		"empty":        "",
	}
	for name, content := range cases {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, configName), content, 0o644)
			// every command reports an invalid config the same way
			for _, cmd := range []string{"init", "apply", "clean", "config", "reset", "convert-patches"} {
				args := []string{cmd}
				if cmd == "config" {
					args = append(args, "-BRF=x")
				}
				r := run(t, dir, "", args...)
				expectCode(t, r, 8)
				expectLog(t, r, "ERROR", "Invalid config file patches.toml")
			}
		})
	}
}

func TestMissingConfig(t *testing.T) {
	dir := t.TempDir()
	for _, cmd := range []string{"apply", "clean", "config", "reset", "convert-patches"} {
		r := run(t, dir, "x\n", cmd)
		expectCode(t, r, 8)
		expectLogLine(t, r, "ERROR d.t.p.c.PatchesConfiguration - No config file patches.toml found. Run patches init first!")
	}
	r := run(t, dir, "x\n", "create-patch")
	expectCode(t, r, 8)
}

func TestInvalidPropertiesConfigIsNotMigrated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches.properties"), "baseRepoUrl=x\n", 0o644)
	r := run(t, dir, "", "init")
	expectCode(t, r, 8)
	expectLog(t, r, "ERROR", "Invalid config file patches.properties")
	if exists(filepath.Join(dir, configName)) || !exists(filepath.Join(dir, "patches.properties")) {
		t.Fatal("an invalid properties file must not be migrated")
	}
}

func TestPropertiesParsing(t *testing.T) {
	// The migration understands the java.util.Properties syntax: ':'
	// separators, whitespace, escapes, comments and line continuations.
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
	expectCode(t, run(t, dir, "", "init"), 0)
	if err := os.Mkdir(filepath.Join(dir, "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "patches", "x.patch"), "content", 0o644)

	r := run(t, dir, "", "config")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.p.cli.command.ConfigCommand - Nothing to change: use --base-repo-url, --base-repo-ref or --patches-directory-path")

	r = run(t, dir, "", "config", "--base-repo-url=https://example.com/repo.git", "-BRF", "v1.2.3", "-PDP", "renamed dir")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing base repo url to 'https://example.com/repo.git' from 'no://op'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing base repo ref to 'v1.2.3' from 'main'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Changing patches directory path to 'renamed dir' from 'patches'")
	expectLogLine(t, r, "INFO  d.t.p.cli.command.ConfigCommand - Saving configuration changes to patches.toml")
	want := "base_repo_url = \"https://example.com/repo.git\"\nbase_repo_ref = \"v1.2.3\"\npatches_directory = \"renamed dir\"\n"
	if got := tomlBody(t, filepath.Join(dir, configName)); got != want {
		t.Fatalf("patches.toml:\n%s\nwant\n%s", got, want)
	}
	if readFile(t, filepath.Join(dir, "renamed dir", "x.patch")) != "content" || exists(filepath.Join(dir, "patches")) {
		t.Fatal("patches directory was not renamed")
	}

	// An existing target directory is never replaced and the config is kept.
	if err := os.Mkdir(filepath.Join(dir, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = run(t, dir, "", "config", "-PDP", "other")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.p.internal.file.FileService - The patches directory other already exists. Move the patches manually!")
	if !strings.Contains(readFile(t, filepath.Join(dir, configName)), "patches_directory = \"renamed dir\"\n") {
		t.Fatal("the config must not change")
	}

	// Without an existing patches directory only the config changes.
	if err := os.RemoveAll(filepath.Join(dir, "renamed dir")); err != nil {
		t.Fatal(err)
	}
	r = run(t, dir, "", "config", "-PDP", "later")
	expectCode(t, r, 0)
	if exists(filepath.Join(dir, "later")) || !strings.Contains(readFile(t, filepath.Join(dir, configName)), "patches_directory = \"later\"\n") {
		t.Fatal("unexpected patches directory change")
	}

	// special characters are escaped (TOML rules)
	r = run(t, dir, "", "config", "-BRF", " a=b:c#d!e\\f\tg h")
	expectCode(t, r, 0)
	if cfg := readFile(t, filepath.Join(dir, configName)); !strings.Contains(cfg, `base_repo_ref = " a=b:c#d!e\\f\tg h"`+"\n") {
		t.Fatalf("unexpected escaping:\n%s", cfg)
	}
}

func TestConfigMigration(t *testing.T) {
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
