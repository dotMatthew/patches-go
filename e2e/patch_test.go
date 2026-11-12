package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createPatch runs create-patch with the given name on stdin.
func createPatch(t *testing.T, f *fixture, name string) string {
	t.Helper()
	r := run(t, f.Project, name+"\n", "create-patch")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.cli.command.CreatePatchCommand - Specify the name of the new patch file: (Without .patch extension)")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Starting to create a patch")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Patch successfully created and saved to disk! ("+name+".patch)")
	return readFile(t, filepath.Join(f.Project, "patches", name+".patch"))
}

// commitInfo returns author, committer and the raw message of a commit.
func commitInfo(t *testing.T, dir, rev string) string {
	return git(t, dir, "log", "-1", "--format=%an <%ae> %ad%n%cn <%ce> %cd%n---%n%B", "--date=raw", rev)
}

// textChanges modifies, deletes and adds text files (incl. special names).
func textChanges(t *testing.T, w string) {
	writeFile(t, filepath.Join(w, "a.txt"), "line1\nchanged\nline3\nline4", 0o644)
	if err := os.Remove(filepath.Join(w, "c.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(w, "n.txt"), "new\n", 0o644)
	writeFile(t, filepath.Join(w, "sub", "dir", "y.txt"), "x\ny\n", 0o644)
	writeFile(t, filepath.Join(w, "empty.txt"), "", 0o644)
	writeFile(t, filepath.Join(w, "spa ce", "ümlaut (1).txt"), "quoted\n", 0o644)
	writeFile(t, filepath.Join(w, "run.sh"), "#!/bin/sh\necho hi\necho there\n", 0o755)
}

func TestCreatePatchAndApplyRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	base := headCommit(t, f.workdir())
	textChanges(t, f.workdir())
	f.commitInWorkdir(t, "Subject here", "Body line 1\nbody line 2")
	original := headCommit(t, f.workdir())

	p := createPatch(t, f, "0001-my-change")
	golden(t, "roundtrip.patch", p)

	// back to the base ref, then apply the patch again
	r := run(t, f.Project, "", "reset")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.ResetCommand - Starting to reset the workdir repository")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Successfully reset the repository")
	if headCommit(t, f.workdir()) != base {
		t.Fatal("reset did not move HEAD back to the base ref")
	}

	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Applying patch 0001-my-change.patch")
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Applied patch 0001-my-change.patch successfully as commit")

	applied := headCommit(t, f.workdir())
	if treeOf(t, f.workdir(), applied) != treeOf(t, f.workdir(), original) {
		t.Fatalf("applied tree differs from original tree:\n%s", git(t, f.workdir(), "diff", original, applied))
	}
	if parent := strings.TrimSpace(git(t, f.workdir(), "rev-parse", "HEAD~1")); parent != base {
		t.Fatalf("patch commit parent = %s, want %s", parent, base)
	}
	wantInfo := "Jane Doe <jane@example.com> 1714984689 -0130\nJane Doe <jane@example.com> 1714984689 -0130\n---\nSubject here\n\nBody line 1\nbody line 2\n\n\n"
	if got := commitInfo(t, f.workdir(), "HEAD"); got != wantInfo {
		t.Fatalf("commit info:\n%q\nwant\n%q", got, wantInfo)
	}
	if statusPorcelain(t, f.workdir()) != "" {
		t.Fatalf("worktree not clean after apply:\n%s", statusPorcelain(t, f.workdir()))
	}
	if fi, err := os.Stat(filepath.Join(f.workdir(), "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatal("run.sh lost its executable bit")
	}

	// applying the same patch again fails
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at applying patch 0001-my-change.patch ")
}

func TestCreatePatchWithoutBody(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	writeFile(t, filepath.Join(f.workdir(), "b.txt"), "keep\nmore\nand more\n", 0o644)
	f.commitInWorkdir(t, "  Only a subject  ", "")
	p := createPatch(t, f, "nobody")
	want := "From: Jane Doe <jane@example.com>\n" +
		"Date: Mon, 6 May 2024 07:08:09 -0130\n" +
		"Subject: Only a subject\n" +
		"\n" +
		"diff --git a/b.txt b/b.txt\n"
	if !strings.HasPrefix(p, want) {
		t.Fatalf("patch header:\n%s", p)
	}
	original := headCommit(t, f.workdir())
	expectCode(t, run(t, f.Project, "", "reset"), 0)
	expectCode(t, run(t, f.Project, "", "apply"), 0)
	if treeOf(t, f.workdir(), "HEAD") != treeOf(t, f.workdir(), original) {
		t.Fatal("tree differs")
	}
	if got := commitInfo(t, f.workdir(), "HEAD"); !strings.HasSuffix(got, "---\nOnly a subject\n\n\n") {
		t.Fatalf("commit message: %q", got)
	}
}

func TestCreatePatchUTCDateAndBinary(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	writeFile(t, filepath.Join(f.workdir(), "bin.dat"), "\x00\x01\x02binary\x00", 0o644)
	writeFile(t, filepath.Join(f.workdir(), "cr.txt"), "lone\rcarriage return\n", 0o644)
	writeFile(t, filepath.Join(f.workdir(), "b.txt"), "keep\nmore\n\n", 0o644)
	git(t, f.workdir(), "add", "-A")
	gitWithEnv(t, f.workdir(), []string{"GIT_AUTHOR_DATE=2023-12-24T18:00:00+0000"}, "commit", "-q", "-m", "binary stuff\n\nwith body\n")
	p := createPatch(t, f, "binary")
	golden(t, "binary.patch", p)

	// "Binary files differ" can not be applied: JGit creates an empty file.
	expectCode(t, run(t, f.Project, "", "reset"), 0)
	expectCode(t, run(t, f.Project, "", "apply"), 0)
	if got := readFile(t, filepath.Join(f.workdir(), "bin.dat")); got != "" {
		t.Fatalf("bin.dat = %q, want empty", got)
	}
	if got := readFile(t, filepath.Join(f.workdir(), "b.txt")); got != "keep\nmore\n\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestCreatePatchDiffAlgorithm(t *testing.T) {
	// Content where the histogram diff (JGit default) and its Myers fallback
	// (for lines occurring more than 64 times) matter.
	f := newFixture(t)
	var oldC, newC strings.Builder
	oldC.WriteString("package x;\n\npublic class A {\n")
	newC.WriteString("package x;\n\nimport y;\n\npublic class A {\n")
	for i := 0; i < 6; i++ {
		oldC.WriteString("    void m" + string(rune('a'+i)) + "() {\n        call();\n    }\n\n")
		if i == 2 {
			newC.WriteString("    void inserted() {\n        call();\n    }\n\n")
		}
		if i != 4 {
			newC.WriteString("    void m" + string(rune('a'+i)) + "() {\n        call();\n    }\n\n")
		}
	}
	oldC.WriteString("}\n")
	newC.WriteString("    // end\n}\n")
	var oldR, newR strings.Builder
	oldR.WriteString("a\n")
	newR.WriteString("d\n")
	for i := 0; i < 70; i++ {
		oldR.WriteString("x\n")
		newR.WriteString("x\n")
	}
	oldR.WriteString("b\n")
	newR.WriteString("e\n")
	for i := 0; i < 70; i++ {
		oldR.WriteString("x\n")
		newR.WriteString("x\n")
	}
	oldR.WriteString("c\n")
	newR.WriteString("f\n")

	writeFile(t, filepath.Join(f.Base, "A.java"), oldC.String(), 0o644)
	writeFile(t, filepath.Join(f.Base, "repeat.txt"), oldR.String(), 0o644)
	git(t, f.Base, "add", ".")
	git(t, f.Base, "commit", "-q", "-m", "java file")
	f.initProject(t, "main")
	writeFile(t, filepath.Join(f.workdir(), "A.java"), newC.String(), 0o644)
	writeFile(t, filepath.Join(f.workdir(), "repeat.txt"), newR.String(), 0o644)
	f.commitInWorkdir(t, "Refactor", "")
	original := headCommit(t, f.workdir())
	p := createPatch(t, f, "algo")
	golden(t, "algorithm.patch", p)

	expectCode(t, run(t, f.Project, "", "reset"), 0)
	expectCode(t, run(t, f.Project, "", "apply"), 0)
	if treeOf(t, f.workdir(), "HEAD") != treeOf(t, f.workdir(), original) {
		t.Fatal("tree differs")
	}
}

func TestCreatePatchErrors(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "x\n", "create-patch")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.p.cli.command.CreatePatchCommand - No valid patches configuration found!")

	f := newFixture(t)
	f.initProject(t, "main")

	// dirty work directory
	writeFile(t, filepath.Join(f.workdir(), "untracked.txt"), "x", 0o644)
	r = run(t, f.Project, "dirty\n", "create-patch")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - Your repository is not in a clean state. Cannot create a patch from an unclean state")
	if err := os.Remove(filepath.Join(f.workdir(), "untracked.txt")); err != nil {
		t.Fatal(err)
	}

	// an ignored file does not make the repository dirty
	writeFile(t, filepath.Join(f.workdir(), "debug.log"), "x", 0o644)
	r = run(t, f.Project, "", "create-patch")
	expectCode(t, r, 0)
	// EOF on stdin: the Java version uses the string "null" as name
	if !exists(filepath.Join(f.Project, "patches", "null.patch")) {
		t.Fatal("null.patch not created")
	}
	if !strings.Contains(readFile(t, filepath.Join(f.Project, "patches", "null.patch")), "Subject: second\n") {
		t.Fatal("unexpected patch content")
	}

	// missing patches directory: the error is logged but the exit code is 0
	if err := os.RemoveAll(filepath.Join(f.Project, "patches")); err != nil {
		t.Fatal(err)
	}
	r = run(t, f.Project, "lost\n", "create-patch")
	expectCode(t, r, 0)
	expectLog(t, r, "ERROR", "Unable to write patch file to disk")

	// empty commit: no diff text
	git(t, f.workdir(), "commit", "-q", "--allow-empty", "-m", "empty")
	r = run(t, f.Project, "empty\n", "create-patch")
	expectCode(t, r, 0)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.DiffService - No diff text found in commit!")
}

func TestCreatePatchOnInitialCommit(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(base, "a"), "a\n", 0o644)
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "only commit")
	expectCode(t, run(t, project, "", "init", "-U", base), 0)
	// HEAD~1 does not exist -> NullPointerException in Java -> exit 1
	r := run(t, project, "p\n", "create-patch")
	expectCode(t, r, 1)
}

func TestApplyWithOffsetsAndHandWrittenPatch(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	// Line numbers in the hunk header are wrong (file starts with line1);
	// JGit searches the hunk position, so this must still apply.
	p := "From: Max Mustermann <max@example.com>\n" +
		"Date: Tue, 3 Jun 2008 11:05:30 GMT\n" +
		"Subject:    Hand written   \n" +
		"X-Ignored: header\n" +
		"\n" +
		"Some text\n\n  indented\n" +
		"diff --git a/a.txt b/a.txt\n" +
		"index 83db48f..0000000 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -5,3 +5,3 @@\n" +
		" line1\n" +
		"-line2\n" +
		"+LINE2\n" +
		" line3\n"
	writeFile(t, filepath.Join(f.Project, "patches", "hand.PATCH"), p, 0o644)
	writeFile(t, filepath.Join(f.Project, "patches", "ignored.txt"), "not a patch", 0o644)
	r := run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	if got := readFile(t, filepath.Join(f.workdir(), "a.txt")); got != "line1\nLINE2\nline3\n" {
		t.Fatalf("a.txt = %q", got)
	}
	want := "Max Mustermann <max@example.com> 1212491130 +0000\nMax Mustermann <max@example.com> 1212491130 +0000\n---\nHand written\n\nSome text\n\n  indented\n\n"
	if got := commitInfo(t, f.workdir(), "HEAD"); got != want {
		t.Fatalf("commit info:\n%q\nwant\n%q", got, want)
	}
}

func TestApplyErrors(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "apply")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.ApplyCommand - No patches config file found! Run patches init first!")

	// invalid config -> NullPointerException is caught -> 42
	writeFile(t, filepath.Join(dir, "patches.properties"), "a=b\n", 0o644)
	r = run(t, dir, "", "apply")
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at applying patches")

	f := newFixture(t)
	f.writeConfig(t, "main")
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.ApplyCommand - Patches directory path is not a directory!")

	if err := os.Mkdir(filepath.Join(f.Project, "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.ApplyCommand - No workdir found!")

	expectCode(t, run(t, f.Project, "", "init"), 0)
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - No patches to apply found")

	// dirty repository
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), "From: A <a@b>\nDate: Tue, 3 Jun 2008 11:05:30 GMT\nSubject: s\n\n", 0o644)
	writeFile(t, filepath.Join(f.workdir(), "a.txt"), "dirty\n", 0o644)
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - The repository is not in a clean state. To apply patches you need a clean repository!")
	git(t, f.workdir(), "checkout", "--", "a.txt")

	// A patch without diff creates an empty commit
	before := headCommit(t, f.workdir())
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	if got := strings.TrimSpace(git(t, f.workdir(), "rev-parse", "HEAD~1")); got != before {
		t.Fatal("no new commit created")
	}
	if treeOf(t, f.workdir(), "HEAD") != treeOf(t, f.workdir(), before) {
		t.Fatal("empty patch changed the tree")
	}

	// unparsable From header
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), "From: no mail address\nSubject: s\n\n", 0o644)
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 42)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.DiffService - Unable to parse from line from patch")

	// unparsable Date header
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), "From: A <a@b>\nDate: yesterday\nSubject: s\n\n", 0o644)
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 42)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.DiffService - Unable to parse date from line from patch")

	// wrong day of week
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), "From: A <a@b>\nDate: Mon, 3 Jun 2008 11:05:30 GMT\nSubject: s\n\n", 0o644)
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 42)

	// missing Date header -> NullPointerException -> 42, but the diff is applied and staged
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), "From: A <a@b>\nSubject: s\n\ndiff --git a/new.txt b/new.txt\nnew file mode 100644\nindex 0000000..3e75765\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+new\n", 0o644)
	head := headCommit(t, f.workdir())
	r = run(t, f.Project, "", "apply")
	expectCode(t, r, 42)
	if headCommit(t, f.workdir()) != head {
		t.Fatal("no commit expected")
	}
	if got := statusPorcelain(t, f.workdir()); got != "A  new.txt\n" {
		t.Fatalf("status = %q", got)
	}
}

func TestApplyBrokenHunk(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	// hunk header promises more lines than the body has
	p := "From: A <a@b>\nDate: Tue, 3 Jun 2008 11:05:30 +0200\nSubject: s\n\n" +
		"diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n line1\n-line2\n+x\n"
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), p, 0o644)
	r := run(t, f.Project, "", "apply")
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at applying patch p.patch ")
	if got := readFile(t, filepath.Join(f.workdir(), "a.txt")); got != "line1\nline2\nline3\n" {
		t.Fatalf("a.txt was modified: %q", got)
	}
}

func TestApplyTwoIndependentPatches(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	writeFile(t, filepath.Join(f.workdir(), "one.txt"), "1\n", 0o644)
	f.commitInWorkdir(t, "one", "")
	createPatch(t, f, "one")
	writeFile(t, filepath.Join(f.workdir(), "two.txt"), "2\n", 0o644)
	f.commitInWorkdir(t, "two", "")
	createPatch(t, f, "two")
	original := headCommit(t, f.workdir())

	expectCode(t, run(t, f.Project, "", "reset"), 0)
	r := run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	if treeOf(t, f.workdir(), "HEAD") != treeOf(t, f.workdir(), original) {
		t.Fatal("tree differs")
	}
	if n := strings.Count(git(t, f.workdir(), "log", "--format=%s", "origin/main..HEAD"), "\n"); n != 2 {
		t.Fatalf("expected 2 commits, got %d", n)
	}
}

func TestApplyCRLFFile(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.Base, "win.txt"), "one\r\ntwo\r\nthree\r\n", 0o644)
	git(t, f.Base, "add", ".")
	git(t, f.Base, "commit", "-q", "-m", "crlf")
	f.initProject(t, "main")
	// The patch format drops CRs (BufferedReader.readLine), JGit converts the
	// CRLF file to LF, applies the patch and writes CRLF again.
	p := "From: A <a@b>\nDate: Tue, 3 Jun 2008 11:05:30 +0200\nSubject: s\n\n" +
		"diff --git a/win.txt b/win.txt\n--- a/win.txt\n+++ b/win.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	writeFile(t, filepath.Join(f.Project, "patches", "p.patch"), p, 0o644)
	expectCode(t, run(t, f.Project, "", "apply"), 0)
	if got := readFile(t, filepath.Join(f.workdir(), "win.txt")); got != "one\r\nTWO\r\nthree\r\n" {
		t.Fatalf("win.txt = %q", got)
	}
}

func TestConvertPatches(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "convert-patches")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.p.c.c.ConvertPatchesCommand - No valid patches configuration file found!")

	f := newFixture(t)
	f.initProject(t, "main")
	r = run(t, f.Project, "", "convert-patches")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.p.c.c.ConvertPatchesCommand - Start to convert our patches to git patches")
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - No patches to convert found")

	textChanges(t, f.workdir())
	f.commitInWorkdir(t, "Subject", "Body")
	p := createPatch(t, f, "conv")
	r = run(t, f.Project, "", "convert-patches")
	expectCode(t, r, 0)
	expectLogLine(t, r, "INFO  d.t.patches.internal.git.GitService - Successfully converted patch conv.patch")
	converted := readFile(t, filepath.Join(f.Project, "converted", "conv.patch"))
	if want := p[strings.Index(p, "diff --git "):]; converted != want {
		t.Fatalf("converted:\n%s\nwant\n%s", converted, want)
	}
	// the converted patch is a valid git patch
	git(t, f.workdir(), "apply", "--check", "-R", filepath.Join(f.Project, "converted", "conv.patch"))
}

func TestResetCleansAndFetches(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	w := f.workdir()
	// local commit + untracked/ignored files
	writeFile(t, filepath.Join(w, "a.txt"), "local\n", 0o644)
	f.commitInWorkdir(t, "local", "")
	for _, p := range []string{"untracked.txt", "dir/sub/f", "ufolder/x.log", "onlyignored/y.log", "build/out", "root.log", "b-dir/z.log"} {
		writeFile(t, filepath.Join(w, p), "x", 0o644)
	}
	if err := os.MkdirAll(filepath.Join(w, "emptydir", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(w, "a.txt"), "modified\n", 0o644)

	// upstream moves on
	writeFile(t, filepath.Join(f.Base, "upstream.txt"), "new upstream\n", 0o644)
	git(t, f.Base, "add", ".")
	git(t, f.Base, "commit", "-q", "-m", "upstream")

	r := run(t, f.Project, "", "reset")
	expectCode(t, r, 0)
	if headCommit(t, w) != headCommit(t, f.Base) {
		t.Fatal("workdir is not at the new upstream main")
	}
	for p, want := range map[string]bool{
		"untracked.txt": false, "dir": false, "ufolder": false, "onlyignored": false, "emptydir": false, "b-dir": false,
		"build/out": true, "root.log": true, "upstream.txt": true,
	} {
		if exists(filepath.Join(w, p)) != want {
			t.Errorf("%s exists = %v, want %v", p, !want, want)
		}
	}
	if got := readFile(t, filepath.Join(w, "a.txt")); got != "line1\nline2\nline3\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if statusPorcelain(t, w) != "" {
		t.Fatalf("not clean: %s", statusPorcelain(t, w))
	}
}

func TestResetToRefs(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	for ref, rev := range map[string]string{
		"v1.0.0":           "v1.0.0",
		"v2.0.0":           "v2.0.0^{commit}",
		"dev":              "dev",
		"refs/tags/v2.0.0": "v2.0.0^{commit}",
		"origin/dev":       "dev",
		strings.TrimSpace(git(t, f.Base, "rev-parse", "v1.0.0")):               "v1.0.0",
		strings.ToUpper(strings.TrimSpace(git(t, f.Base, "rev-parse", "dev"))): "dev",
	} {
		r := run(t, f.Project, "", "config", "-BRF", ref)
		expectCode(t, r, 0)
		r = run(t, f.Project, "", "reset")
		expectCode(t, r, 0)
		want := strings.TrimSpace(git(t, f.Base, "rev-parse", rev))
		if got := headCommit(t, f.workdir()); got != want {
			t.Errorf("ref %s: HEAD = %s, want %s", ref, got, want)
		}
	}
}

func TestResetErrors(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "reset")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.cli.command.ResetCommand - Unable to reset repository. No config file found")

	f := newFixture(t)
	f.writeConfig(t, "main")
	r = run(t, f.Project, "", "reset")
	expectCode(t, r, 8)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - No git directory found to reset!")

	expectCode(t, run(t, f.Project, "", "init"), 0)
	expectCode(t, run(t, f.Project, "", "config", "-BRF", "unknown-ref"), 0)
	r = run(t, f.Project, "", "reset")
	expectCode(t, r, 42)
	expectLogLine(t, r, "ERROR d.t.patches.internal.git.GitService - Could not found commit id for base ref")

	expectCode(t, run(t, f.Project, "", "config", "-BRF", "0123456789012345678901234567890123456789"), 0)
	r = run(t, f.Project, "", "reset")
	expectCode(t, r, 42)

	// not a git repository
	if err := os.RemoveAll(f.workdir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.workdir(), 0o755); err != nil {
		t.Fatal(err)
	}
	r = run(t, f.Project, "", "reset")
	expectCode(t, r, 42)
	expectLog(t, r, "ERROR", "An error occurred at resetting repository")
}

func TestClean(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, "", "clean")
	expectCode(t, r, 42)

	f := newFixture(t)
	f.writeConfig(t, "main")
	r = run(t, f.Project, "", "clean")
	expectCode(t, r, 1)
	expectLogLine(t, r, "INFO  d.t.patches.cli.command.CleanCommand - Deleting _workdir directory")
	expectLogLine(t, r, "ERROR d.t.p.internal.file.FileService - Git repo dir specified in config is not a directory or does not exist?")

	expectCode(t, run(t, f.Project, "", "init"), 0)
	writeFile(t, filepath.Join(f.workdir(), "build", "ignored"), "x", 0o644)
	r = run(t, f.Project, "", "clean")
	expectCode(t, r, 0)
	if exists(f.workdir()) {
		t.Fatal("_workdir still exists")
	}
	if !exists(filepath.Join(f.Project, "patches.properties")) || !exists(filepath.Join(f.Project, "patches")) {
		t.Fatal("clean removed too much")
	}

	// invalid config -> NullPointerException -> exit 1
	writeFile(t, filepath.Join(f.Project, "patches.properties"), "", 0o644)
	expectCode(t, run(t, f.Project, "", "clean"), 1)
}
