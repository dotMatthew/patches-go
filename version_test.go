package main

import "testing"

func TestVersionFromBuildInfo(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2.3":                               "1.2.3",
		"v1.2.4-0.20260101120000-abcdef123456": "1.2.4-0.20260101120000-abcdef123456",
		"v1.2.3+dirty":                         "1.2.3+dirty",
		"(devel)":                              "dev",
		"":                                     "dev",
	} {
		if got := versionFromBuildInfo(in); got != want {
			t.Errorf("versionFromBuildInfo(%q) = %q, want %q", in, got, want)
		}
	}
}
