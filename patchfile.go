package main

import (
	"errors"
	"os"
	"strings"
	"time"
)

// patchFile is a patch with a mail like header:
//
//	From: Name <mail>
//	Date: <RFC 1123 date>
//	Subject: <subject>
//
//	<body>
//
//	diff --git ...
type patchFile struct {
	Diff, Body  string
	Subject     string // "null" if missing (Java concatenates the null reference)
	Name, Email string
	HasAuthor   bool      // a From header was found
	Date        time.Time // zero if missing
}

const rfc1123 = "Mon, 2 Jan 2006 15:04:05 -0700"

// writePatch writes a patch file.
func writePatch(file, diffText, subject, body, name, email string, date time.Time) error {
	switch {
	case strings.TrimSpace(diffText) == "":
		return failure(exitInternal, logPatch, nil, "No diff text found in commit!")
	case !strings.HasPrefix(diffText, "diff --git "):
		return failure(exitInternal, logPatch, nil, "The specified diff is incorrect!")
	case strings.TrimSpace(name) == "":
		return failure(exitInternal, logPatch, nil, "The name of the author is null or blank!")
	case strings.TrimSpace(email) == "":
		return failure(exitInternal, logPatch, nil, "The email of the author is null or blank!")
	}
	dateText := date.Format(rfc1123)
	if _, offset := date.Zone(); offset == 0 {
		dateText = strings.TrimSuffix(dateText, "+0000") + "GMT"
	}
	var sb strings.Builder
	sb.WriteString("From: " + name + " <" + email + ">\nDate: " + dateText + "\nSubject: " + strings.TrimSpace(subject) + "\n\n")
	if body = strings.TrimSpace(body); body != "" {
		sb.WriteString(body + "\n\n")
	}
	sb.WriteString(diffText)
	if !strings.HasSuffix(diffText, "\n") {
		sb.WriteString("\n")
	}
	if err := os.WriteFile(file, []byte(sb.String()), 0o644); err != nil {
		return failure(exitCrash, logPatch, err, "Unable to write patch file to disk")
	}
	return nil
}

// readPatch parses a patch file.
func readPatch(file string) (*patchFile, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, failure(exitInternal, logPatch, err, "Unable to read patch files from disk")
	}
	p := &patchFile{Subject: "null"}
	var diff, body strings.Builder
	section := "headers"
	for _, line := range readLines(toValidUTF8(raw)) {
		if section != "diff" && strings.HasPrefix(line, "diff --git ") {
			section = "diff"
		}
		switch section {
		case "diff":
			diff.WriteString(line + "\n")
		case "body":
			body.WriteString(line + "\n")
		case "headers":
			if strings.TrimSpace(line) == "" {
				section = "body"
			} else if v, ok := strings.CutPrefix(line, "From: "); ok {
				// Java: split("<") must yield exactly two parts (trailing empty parts are dropped)
				parts := strings.Split(v, "<")
				for len(parts) > 0 && parts[len(parts)-1] == "" {
					parts = parts[:len(parts)-1]
				}
				if len(parts) != 2 {
					return nil, failure(exitInternal, logPatch, nil, "Unable to parse from line from patch")
				}
				p.Name = strings.TrimSpace(parts[0])
				p.Email = strings.ReplaceAll(strings.TrimSpace(parts[1]), ">", "")
				p.HasAuthor = true
			} else if v, ok := strings.CutPrefix(line, "Date: "); ok {
				if p.Date, err = parseDate(strings.TrimSpace(v)); err != nil {
					return nil, failure(exitInternal, logPatch, nil, "Unable to parse date from line from patch")
				}
			} else if v, ok := strings.CutPrefix(line, "Subject: "); ok {
				p.Subject = strings.TrimSpace(v)
			}
		}
	}
	p.Diff, p.Body = diff.String(), body.String()
	return p, nil
}

// parseDate parses an RFC 1123 date ("[Tue, ]3 Jun 2008 11:05[:30] GMT|+0200").
func parseDate(s string) (time.Time, error) {
	weekday, rest, hasWeekday := strings.Cut(s, ", ")
	if !hasWeekday {
		rest = s
	}
	if strings.HasSuffix(strings.ToUpper(rest), " GMT") {
		rest = rest[:len(rest)-3] + "+0000"
	}
	for _, layout := range []string{"2 Jan 2006 15:04:05 -0700", "2 Jan 2006 15:04 -0700"} {
		t, err := time.Parse(layout, rest)
		if err != nil {
			continue
		}
		if hasWeekday && !strings.EqualFold(weekday, t.Weekday().String()[:3]) {
			break
		}
		return t, nil
	}
	return time.Time{}, errors.New("invalid date")
}

// toValidUTF8 replaces every invalid byte with U+FFFD.
func toValidUTF8(b []byte) string { return string([]rune(string(b))) }

// readLines splits like Java's BufferedReader.readLine: at "\n", "\r" or "\r\n".
func readLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
