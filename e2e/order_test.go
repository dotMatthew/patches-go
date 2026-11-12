package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// chainPatch replaces the first line of a.txt (from -> to).
func chainPatch(subject, from, to string) string {
	return "From: A <a@example.com>\nDate: Tue, 3 Jun 2008 11:05:30 +0200\nSubject: " + subject + "\n\n" +
		"diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n" +
		"-" + from + "\n+" + to + "\n line2\n line3\n"
}

// Patches are applied ordered by their numeric prefix ("00001_", "002_",
// "9_", "10_"), files without a number come last. Every patch depends on
// the previous one, so any other order fails.
func TestApplyOrderByNumberPrefix(t *testing.T) {
	f := newFixture(t)
	f.initProject(t, "main")
	patches := map[string]string{
		"after.patch":       chainPatch("after", "ten", "final"),
		"10_third.patch":    chainPatch("third", "nine", "ten"),
		"9_ninth.patch":     chainPatch("ninth", "two", "nine"),
		"002_second.patch":  chainPatch("second", "one", "two"),
		"00001_first.patch": chainPatch("first", "line1", "one"),
	}
	for name, content := range patches {
		writeFile(t, filepath.Join(f.Project, "patches", name), content, 0o644)
	}

	r := run(t, f.Project, "", "apply")
	expectCode(t, r, 0)
	var applied []string
	for _, l := range r.Logs() {
		if _, name, ok := strings.Cut(l, " - Applying patch "); ok {
			applied = append(applied, name)
		}
	}
	want := []string{"00001_first.patch", "002_second.patch", "9_ninth.patch", "10_third.patch", "after.patch"}
	if strings.Join(applied, ",") != strings.Join(want, ",") {
		t.Fatalf("apply order = %v, want %v", applied, want)
	}
	if got := readFile(t, filepath.Join(f.workdir(), "a.txt")); got != "final\nline2\nline3\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if got := git(t, f.workdir(), "log", "--format=%s", "origin/main..HEAD"); got != "after\nthird\nninth\nsecond\nfirst\n" {
		t.Fatalf("commits:\n%s", got)
	}

	// convert-patches uses the same order
	r = run(t, f.Project, "", "convert-patches")
	expectCode(t, r, 0)
	var converted []string
	for _, l := range r.Logs() {
		if _, name, ok := strings.Cut(l, " - Successfully converted patch "); ok {
			converted = append(converted, name)
		}
	}
	if strings.Join(converted, ",") != strings.Join(want, ",") {
		t.Fatalf("convert order = %v, want %v", converted, want)
	}
}
