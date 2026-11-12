package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// newTestApp returns an app working in a temporary directory.
func newTestApp(t *testing.T, stdin string) (*app, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &app{ctx: context.Background(), dir: t.TempDir(), stdin: strings.NewReader(stdin),
		stdout: &stdout, stderr: &stderr}, &stdout, &stderr
}

func TestInitCreatesExampleConfig(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if code := a.run([]string{"init"}); code != exitOK {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stdout.String(), "Created the config file patches.toml.") {
		t.Fatalf("unexpected output %q", stdout)
	}
	c, err := loadConfig(filepath.Join(a.dir, defaultConfigFile))
	if err != nil || *c != (config{URL: exampleURL, Ref: "main", PatchesDir: "patches"}) {
		t.Fatalf("config %+v, %v", c, err)
	}
}

func TestInvalidConfigIsReported(t *testing.T) {
	a, stdout, stderr := newTestApp(t, "")
	if err := os.WriteFile(filepath.Join(a.dir, defaultConfigFile), []byte("base_repo_url = \"u\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := a.run([]string{"reset"}); code != exitUsage {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stdout.String(), "ERROR d.t.p.c.PatchesConfiguration - Invalid config file patches.toml\nbase_repo_url and base_repo_ref are required\n") ||
		stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
}

func TestProperties(t *testing.T) {
	in := "! comment\n# comment\n  key1 : value one\nkey2   multi\\\n     line\nkey\\ 3=a\\=b\\u00fc\\t\nempty\n"
	got, err := parseProperties([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"key1": "value one", "key2": "multiline", "key 3": "a=bü\t", "empty": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	if _, err := parseProperties([]byte("k=\\u12")); err == nil {
		t.Error("malformed \\u escape must fail")
	}
}

func TestParseDate(t *testing.T) {
	for in, want := range map[string]string{
		"Tue, 3 Jun 2008 11:05:30 GMT":   "2008-06-03T11:05:30Z",
		"3 Jun 2008 11:05:30 +0200":      "2008-06-03T11:05:30+02:00",
		"Mon, 6 May 2024 07:08:09 -0130": "2024-05-06T07:08:09-01:30",
		"tue, 03 jun 2008 11:05 gmt":     "2008-06-03T11:05:00Z",
	} {
		got, err := parseDate(in)
		if err != nil || got.Format(time.RFC3339) != want {
			t.Errorf("parseDate(%q) = %v, %v", in, got.Format(time.RFC3339), err)
		}
	}
	for _, in := range []string{"Mon, 3 Jun 2008 11:05:30 GMT", "yesterday", "3 Jun 2008 11:05:30 CET"} {
		if _, err := parseDate(in); err == nil {
			t.Errorf("parseDate(%q) should fail", in)
		}
	}
}

func TestComparePatchNames(t *testing.T) {
	names := []string{"b.patch", "10_x.patch", "a.patch", "002_y.patch", "9_z.patch", "00001_w.patch", "1_v.patch"}
	slices.SortFunc(names, comparePatchNames)
	want := []string{"00001_w.patch", "1_v.patch", "002_y.patch", "9_z.patch", "10_x.patch", "a.patch", "b.patch"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v", names)
	}
}

func TestReadLine(t *testing.T) {
	for in, want := range map[string]string{"name\nrest": "name", "name\r\n": "name", "last": "last", "": "", "\n": ""} {
		if got, err := readLine(strings.NewReader(in)); err != nil || got != want {
			t.Errorf("readLine(%q) = %q, %v", in, got, err)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigTOML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "patches.toml")
	c := &config{URL: "https://example.com/r.git", Ref: "v1.0.0", PatchesDir: "my patches"}
	if err := c.save(file); err != nil {
		t.Fatal(err)
	}
	want := "# patches configuration\nbase_repo_url = \"https://example.com/r.git\"\nbase_repo_ref = \"v1.0.0\"\npatches_directory = \"my patches\"\n"
	if got := readFile(t, file); got != want {
		t.Fatalf("saved:\n%s\nwant:\n%s", got, want)
	}
	if got, err := loadConfig(file); err != nil || *got != *c {
		t.Fatalf("loaded %+v, %v", got, err)
	}

	writeFile(t, file, "base_repo_url = \"u\"\nbase_repo_ref = \"r\"\n")
	if got, err := loadConfig(file); err != nil || got.PatchesDir != "patches" {
		t.Fatalf("default patches directory: %+v, %v", got, err)
	}
	writeFile(t, file, "base_repo_url = \"u\"\nbase_repo_ref = \"r\"\ntypo = 1\n")
	if _, err := loadConfig(file); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("unknown key must be rejected: %v", err)
	}
}

func TestMigration(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	old := filepath.Join(a.dir, "patches.properties")
	writeFile(t, old, "baseRepoUrl=https\\://example.com/r.git\nbaseRepoRef=main\npatchesDirectoryPath=p\n")

	got := a.resolveConfigFile(filepath.Join(a.dir, defaultConfigFile))
	if got != filepath.Join(a.dir, "patches.toml") {
		t.Fatalf("resolved %s", got)
	}
	c, err := loadConfig(got)
	if err != nil || *c != (config{URL: "https://example.com/r.git", Ref: "main", PatchesDir: "p"}) {
		t.Fatalf("migrated config %+v, %v", c, err)
	}
	if exists(old) || !exists(old+".bak") {
		t.Fatal("the old file must be renamed to .bak")
	}
	if !strings.Contains(stdout.String(), "INFO  d.t.p.c.PatchesConfiguration - Migrated patches.properties to patches.toml (the old file was renamed to patches.properties.bak)") {
		t.Fatalf("log: %s", stdout)
	}
}

func TestMigrationWithExplicitFile(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	old := filepath.Join(a.dir, "custom.properties")
	writeFile(t, old, "baseRepoUrl=u\nbaseRepoRef=r\n")
	if got := a.resolveConfigFile(old); got != filepath.Join(a.dir, "custom.toml") {
		t.Fatalf("resolved %s", got)
	}
	// once migrated, the TOML file is used directly
	writeFile(t, old, "baseRepoUrl=other\nbaseRepoRef=r\n")
	if got := a.resolveConfigFile(old); got != filepath.Join(a.dir, "custom.toml") {
		t.Fatalf("resolved %s", got)
	}
	if c, _ := loadConfig(filepath.Join(a.dir, "custom.toml")); c.URL != "u" {
		t.Fatalf("the TOML file must not be overwritten: %+v", c)
	}
}

func TestInvalidPropertiesAreNotMigrated(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	old := filepath.Join(a.dir, "patches.properties")
	writeFile(t, old, "baseRepoUrl=u\n")
	if got := a.resolveConfigFile(filepath.Join(a.dir, defaultConfigFile)); got != old {
		t.Fatalf("resolved %s, want the old file", got)
	}
	if exists(filepath.Join(a.dir, "patches.toml")) || !exists(old) {
		t.Fatal("an invalid config must not be migrated")
	}
}
