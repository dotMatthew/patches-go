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
	if !strings.Contains(stdout.String(), "Create new base patches config file! Happy working :)") {
		t.Fatalf("unexpected output %q", stdout)
	}
	c, err := loadConfig(filepath.Join(a.dir, defaultConfigFile))
	if err != nil || *c != (config{URL: exampleURL, Ref: "main", PatchesDir: "patches"}) {
		t.Fatalf("config %+v, %v", c, err)
	}
}

func TestInvalidConfigIsReported(t *testing.T) {
	a, stdout, stderr := newTestApp(t, "")
	if err := os.WriteFile(filepath.Join(a.dir, defaultConfigFile), []byte("x=y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := a.run([]string{"reset"}); code != exitCrash {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stdout.String(), "INFO  d.t.p.c.PatchesConfiguration - Keys cannot be loaded") ||
		!strings.HasPrefix(stderr.String(), "Exception: ") {
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
	if got := escapeProperty(" a=b:c#d!e\\f\tü"); got != `\ a\=b\:c\#d\!e\\f\t`+"\\u00FC" {
		t.Errorf("escapeProperty = %s", got)
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
	for in, want := range map[string]string{"name\nrest": "name", "name\r\n": "name", "last": "last", "": "null", "\n": ""} {
		if got, err := readLine(strings.NewReader(in)); err != nil || got != want {
			t.Errorf("readLine(%q) = %q, %v", in, got, err)
		}
	}
}
