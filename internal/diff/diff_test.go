package diff

import (
	"bytes"
	"strings"
	"testing"
)

// checkEdits verifies that the edits turn a into b and that every line
// between the edits is equal in both texts.
func checkEdits(t *testing.T, a, b *RawText, edits []*Edit) {
	t.Helper()
	posA, posB := 0, 0
	for _, e := range edits {
		if e.BeginA < posA || e.BeginB < posB || e.BeginA-posA != e.BeginB-posB {
			t.Fatalf("edits out of order or inconsistent: %+v", *e)
		}
		for ; posA < e.BeginA; posA, posB = posA+1, posB+1 {
			if !equals(a, posA, b, posB) {
				t.Fatalf("unchanged line %d/%d differs", posA, posB)
			}
		}
		posA, posB = e.EndA, e.EndB
	}
	if a.Size()-posA != b.Size()-posB {
		t.Fatalf("trailing lines do not match: %d vs %d", a.Size()-posA, b.Size()-posB)
	}
	for ; posA < a.Size(); posA, posB = posA+1, posB+1 {
		if !equals(a, posA, b, posB) {
			t.Fatalf("unchanged trailing line %d/%d differs", posA, posB)
		}
	}
}

func TestDiffAlgorithms(t *testing.T) {
	repeated := strings.Repeat("x\n", 70)
	cases := []struct{ a, b string }{
		{"", ""},
		{"", "a\n"},
		{"a\n", ""},
		{"a\nb\nc\n", "a\nc\n"},
		{"a\nb\nc\n", "a\nB\nc\nd"},
		{"a", "a\n"},
		{"a\nb\na\nb\na\n", "b\na\nb\na\nb\n"},
		// only lines occurring more than 64 times are common: Myers fallback
		{"a\n" + repeated + "b\n" + repeated + "c\n", "d\n" + repeated + "e\n" + repeated + "f\n"},
	}
	for _, alg := range []Algorithm{Histogram, Myers} {
		for _, tc := range cases {
			a, b := NewRawText([]byte(tc.a)), NewRawText([]byte(tc.b))
			checkEdits(t, a, b, Diff(alg, a, b))
		}
	}
}

func FuzzDiff(f *testing.F) {
	f.Add("a\nb\nc\n", "a\nc\nd\n")
	f.Add("x\nx\nx\ny\n", "y\nx\nx\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, alg := range []Algorithm{Histogram, Myers} {
			ra, rb := NewRawText([]byte(a)), NewRawText([]byte(b))
			checkEdits(t, ra, rb, Diff(alg, ra, rb))
		}
	})
}

func TestFormat(t *testing.T) {
	blobs := map[string]string{
		"83db48f0000000000000000000000000000000000": "line1\nline2\nline3\n",
		"aca3ea50000000000000000000000000000000000": "line1\nchanged\nline3\nline4",
	}
	f := NewFormatter()
	f.LoadBlob = func(id string) ([]byte, error) { return []byte(blobs[id]), nil }
	var out bytes.Buffer
	err := f.Format(&out, &Entry{Type: Modify, OldPath: "a.txt", NewPath: "a.txt", OldMode: ModeRegular, NewMode: ModeRegular,
		OldID: "83db48f0000000000000000000000000000000000", NewID: "aca3ea50000000000000000000000000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	want := `diff --git a/a.txt b/a.txt
index 83db48f..aca3ea5 100644
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,4 @@
 line1
-line2
+changed
 line3
+line4
\ No newline at end of file
`
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestQuotePath(t *testing.T) {
	for in, want := range map[string]string{
		"a/b c.txt":      "a/b c.txt",
		"a/ümlaut (1).x": `"a/\303\274mlaut \0501\051.x"`,
		"tab\there":      `"tab\there"`,
		"":               `""`,
	} {
		if got := QuotePath(in, true); got != want {
			t.Errorf("QuotePath(%q) = %s, want %s", in, got, want)
		}
	}
	if got := QuotePath("ü", false); got != "ü" {
		t.Errorf("QuotePath without quoteHigh = %s", got)
	}
}

func TestIsBinary(t *testing.T) {
	for in, want := range map[string]bool{"text\n": false, "nul\x00": true, "lone\rcr": true, "crlf\r\n": false, "end\r": true} {
		if got := IsBinary([]byte(in)); got != want {
			t.Errorf("IsBinary(%q) = %v", in, got)
		}
	}
}
