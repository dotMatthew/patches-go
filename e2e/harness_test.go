// Package e2e contains black box tests for the patches CLI. Without
// PATCHES_CMD the Go binary of this repository is built and tested. Any other
// implementation (e.g. the original Java version) can be tested with the
// same tests by setting PATCHES_CMD:
//
//	go test ./e2e
//	PATCHES_CMD="java -jar /path/to/patches.jar" go test ./e2e
//
// Golden files in testdata/ can be (re)generated with UPDATE_GOLDEN=1.
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var patchesCmd []string

func TestMain(m *testing.M) {
	cmd := strings.Fields(os.Getenv("PATCHES_CMD"))
	if len(cmd) == 0 {
		bin, err := buildBinary()
		if err != nil {
			fmt.Fprintln(os.Stderr, "building patches:", err)
			os.Exit(2)
		}
		cmd = []string{bin}
	}
	// Resolve relative paths against the e2e directory.
	for i, c := range cmd {
		if strings.HasPrefix(c, "./") || strings.HasPrefix(c, "../") {
			abs, err := filepath.Abs(c)
			if err == nil {
				cmd[i] = abs
			}
		}
	}
	patchesCmd = cmd
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git is required to build the test fixtures")
		os.Exit(2)
	}
	code := m.Run()
	if len(os.Getenv("PATCHES_CMD")) == 0 {
		os.RemoveAll(filepath.Dir(cmd[0]))
	}
	os.Exit(code)
}

// buildBinary builds the patches command of this repository.
func buildBinary() (string, error) {
	dir, err := os.MkdirTemp("", "patches-e2e")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "patches")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v\n%s", err, out)
	}
	return bin, nil
}

// result of a CLI invocation.
type result struct {
	Stdout, Stderr string
	Code           int
}

// Logs returns the log lines of stdout without the timestamp prefix.
func (r result) Logs() []string {
	var out []string
	for _, l := range strings.Split(r.Stdout, "\n") {
		if m := tsPrefix.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

var tsPrefix = regexp.MustCompile(`^\d\d:\d\d:\d\d\.\d\d\d \[main\] (.*)$`)

// run executes the CLI in dir.
func run(t *testing.T, dir, stdin string, args ...string) result {
	t.Helper()
	full := append(append([]string{}, patchesCmd[1:]...), args...)
	cmd := exec.Command(patchesCmd[0], full...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "TZ=UTC", "LANG=C.UTF-8", "LC_ALL=C.UTF-8")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %v: %v", full, err)
		}
		code = ee.ExitCode()
	}
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

func expectCode(t *testing.T, r result, code int) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", r.Code, code, r.Stdout, r.Stderr)
	}
}

// expectLog checks that a log line with the given level and message exists.
func expectLog(t *testing.T, r result, level, msg string) {
	t.Helper()
	for _, l := range r.Logs() {
		if strings.HasPrefix(l, level) && strings.HasSuffix(l, " - "+msg) {
			return
		}
	}
	t.Fatalf("missing log %s %q in output:\n%s", level, msg, r.Stdout)
}

// expectLogLine checks the complete log line (level, logger and message).
func expectLogLine(t *testing.T, r result, line string) {
	t.Helper()
	for _, l := range r.Logs() {
		if l == line {
			return
		}
	}
	t.Fatalf("missing log line %q in output:\n%s", line, r.Stdout)
}

// ------------------------------------------------------------------ git

// gitEnv makes commits deterministic.
func gitEnv(date string) []string {
	if date == "" {
		date = "2024-01-02T03:04:05+0200"
	}
	return []string{
		"GIT_AUTHOR_NAME=Base Author", "GIT_AUTHOR_EMAIL=base@example.com",
		"GIT_COMMITTER_NAME=Base Author", "GIT_COMMITTER_EMAIL=base@example.com",
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"LANG=C", "LC_ALL=C",
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gitWithEnv(t, dir, nil, args...)
}

func gitWithEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), gitEnv("")...), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
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

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// fixture is a base repository plus a project directory.
type fixture struct {
	Base    string // upstream repository
	Project string // directory the CLI runs in
}

// newFixture creates an upstream repository with branch main (two commits),
// branch dev, a lightweight tag v1.0.0 and an annotated tag v2.0.0.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{Base: filepath.Join(root, "base"), Project: filepath.Join(root, "project")}
	for _, d := range []string{f.Base, f.Project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, f.Base, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(f.Base, "a.txt"), "line1\nline2\nline3\n", 0o644)
	writeFile(t, filepath.Join(f.Base, "b.txt"), "keep\n", 0o644)
	writeFile(t, filepath.Join(f.Base, "c.txt"), "gone\n", 0o644)
	writeFile(t, filepath.Join(f.Base, "run.sh"), "#!/bin/sh\necho hi\n", 0o644)
	writeFile(t, filepath.Join(f.Base, ".gitignore"), "*.log\nbuild/\n", 0o644)
	git(t, f.Base, "add", ".")
	git(t, f.Base, "commit", "-q", "-m", "initial")
	git(t, f.Base, "tag", "v1.0.0")
	writeFile(t, filepath.Join(f.Base, "b.txt"), "keep\nmore\n", 0o644)
	git(t, f.Base, "commit", "-q", "-am", "second")
	git(t, f.Base, "tag", "-a", "-m", "release 2", "v2.0.0")
	git(t, f.Base, "checkout", "-q", "-b", "dev")
	writeFile(t, filepath.Join(f.Base, "d.txt"), "dev\n", 0o644)
	git(t, f.Base, "add", ".")
	git(t, f.Base, "commit", "-q", "-m", "dev work")
	git(t, f.Base, "checkout", "-q", "main")
	return f
}

func (f *fixture) workdir() string { return filepath.Join(f.Project, "_workdir") }

// writeConfig writes patches.properties in the project directory.
func (f *fixture) writeConfig(t *testing.T, ref string) {
	t.Helper()
	// The Java Properties format needs ':' and '\' escaped.
	url := strings.NewReplacer(`\`, `\\`, ":", `\:`).Replace(f.Base)
	writeFile(t, filepath.Join(f.Project, "patches.properties"),
		"baseRepoUrl="+url+"\nbaseRepoRef="+ref+"\npatchesDirectoryPath=patches\n", 0o644)
}

// initProject writes a config and runs `patches init`.
func (f *fixture) initProject(t *testing.T, ref string) {
	t.Helper()
	f.writeConfig(t, ref)
	r := run(t, f.Project, "", "init")
	expectCode(t, r, 0)
}

// commitInWorkdir commits all changes in _workdir with a fixed author.
func (f *fixture) commitInWorkdir(t *testing.T, subject, body string) {
	t.Helper()
	git(t, f.workdir(), "add", "-A")
	args := []string{"commit", "-q", "-m", subject}
	if body != "" {
		args = append(args, "-m", body)
	}
	gitWithEnv(t, f.workdir(), []string{
		"GIT_AUTHOR_NAME=Jane Doe", "GIT_AUTHOR_EMAIL=jane@example.com",
		"GIT_AUTHOR_DATE=2024-05-06T07:08:09-0130",
	}, args...)
}

func headCommit(t *testing.T, dir string) string {
	return strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
}

func treeOf(t *testing.T, dir, rev string) string {
	return strings.TrimSpace(git(t, dir, "rev-parse", rev+"^{tree}"))
}

func statusPorcelain(t *testing.T, dir string) string {
	return git(t, dir, "status", "--porcelain")
}

// golden compares content with testdata/<name>; UPDATE_GOLDEN=1 rewrites it.
func golden(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		writeFile(t, path, content, 0o644)
		return
	}
	want := readFile(t, path)
	if want != content {
		t.Fatalf("content differs from golden file %s\n--- want\n%s\n--- got\n%s", path, want, content)
	}
}
