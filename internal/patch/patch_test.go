package patch

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotMatthew/patches-go/internal/diff"
)

const (
	oldID = "1111111111111111111111111111111111111111"
	newID = "2222222222222222222222222222222222222222"
)

// formatDiff returns the git diff that turns a into b for file f.txt.
func formatDiff(t testing.TB, a, b string) string {
	f := diff.NewFormatter()
	f.LoadBlob = func(id string) ([]byte, error) {
		if id == oldID {
			return []byte(a), nil
		}
		return []byte(b), nil
	}
	var out bytes.Buffer
	err := f.Format(&out, &diff.Entry{Type: diff.Modify, OldPath: "f.txt", NewPath: "f.txt",
		OldMode: diff.ModeRegular, NewMode: diff.ModeRegular, OldID: oldID, NewID: newID})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// applyTo writes content to f.txt in a new directory, applies the patch and
// returns the result.
func applyTo(t testing.TB, content, patchText string) (string, error) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(dir, patchText); err != nil {
		return "", err
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(got), nil
}

func TestRoundTrip(t *testing.T) {
	cases := []struct{ a, b string }{
		{"line1\nline2\nline3\n", "line1\nchanged\nline3\nline4"},
		{"a\n", "b\n"},
		{"a\nb\nc\n", ""},
		{"no newline", "no newline\n"},
		{"", "new\ncontent\n"},
		{strings.Repeat("ctx\n", 20) + "x\n" + strings.Repeat("ctx\n", 20), strings.Repeat("ctx\n", 20) + "y\n" + strings.Repeat("ctx\n", 21)},
	}
	for _, tc := range cases {
		got, err := applyTo(t, tc.a, formatDiff(t, tc.a, tc.b))
		if err != nil || got != tc.b {
			t.Errorf("%q -> %q: got %q, %v", tc.a, tc.b, got, err)
		}
	}
}

// FuzzRoundTrip checks that applying the generated diff of a -> b to a
// always yields b. Binary content and CR characters are excluded: binary
// files are not diffed and CRLF handling deliberately follows JGit.
func FuzzRoundTrip(f *testing.F) {
	f.Add("a\nb\nc\n", "a\nc\nd")
	f.Add("x\nx\nx\n", "x\ny\nx\nx\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		if a == b || strings.ContainsRune(a+b, '\r') || diff.IsBinary([]byte(a)) || diff.IsBinary([]byte(b)) {
			t.Skip()
		}
		got, err := applyTo(t, a, formatDiff(t, a, b))
		if err != nil {
			t.Fatalf("apply failed: %v", err)
		}
		if got != b {
			t.Fatalf("got %q, want %q", got, b)
		}
	})
}

func TestApplyFindsMovedHunk(t *testing.T) {
	// The hunk says line 5, the context is at line 2.
	p := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -5,3 +5,3 @@\n a\n-b\n+B\n c\n"
	got, err := applyTo(t, "x\na\nb\nc\n", p)
	if err != nil || got != "x\na\nB\nc\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestApplyKeepsCRLF(t *testing.T) {
	p := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n+B\n"
	got, err := applyTo(t, "a\r\nb\r\n", p)
	if err != nil || got != "a\r\nB\r\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestApplyAddDelete(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := "diff --git a/old.txt b/old.txt\ndeleted file mode 100644\n--- a/old.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n" +
		"diff --git a/sub/new.sh b/sub/new.sh\nnew file mode 100755\n--- /dev/null\n+++ b/sub/new.sh\n@@ -0,0 +1 @@\n+echo\n"
	written, err := Apply(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "sub/new.sh" {
		t.Fatalf("written = %v", written)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old.txt not deleted")
	}
	fi, err := os.Stat(filepath.Join(dir, "sub", "new.sh"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("new.sh missing or not executable: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	for name, p := range map[string]string{
		"truncated":    "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-b\n+c\n",
		"too long":     "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n a\n b\n",
		"disconnected": "@@ -1 +1 @@\n-a\n+b\n",
		"git binary":   "diff --git a/f b/f\nGIT binary patch\nliteral 1\n",
	} {
		if _, err := Parse(p); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseNames(t *testing.T) {
	files, err := Parse("diff --git \"a/sp ace/\\303\\274.txt\" \"b/sp ace/\\303\\274.txt\"\nold mode 100644\nnew mode 100755\n")
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if f.OldPath != "sp ace/ü.txt" || f.NewPath != f.OldPath || f.NewMode != 0o100755 || f.Type != Modify {
		t.Fatalf("parsed %+v", *f)
	}
}
