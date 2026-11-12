package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Subject     string
	Name, Email string
	Date        time.Time
}

const rfc1123 = "Mon, 2 Jan 2006 15:04:05 -0700"

// writePatch writes a patch file.
func writePatch(file, diffText, subject, body, name, email string, date time.Time) error {
	switch {
	case strings.TrimSpace(diffText) == "":
		return failure(exitUsage, logPatch, nil, "The HEAD commit contains no changes")
	case !strings.HasPrefix(diffText, "diff --git "):
		return failure(exitInternal, logPatch, nil, "The generated diff is invalid")
	case strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "":
		return failure(exitUsage, logPatch, nil, "The author of the HEAD commit has no name or email")
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
		return failure(exitIO, logPatch, err, "Unable to write the patch file %s", filepath.Base(file))
	}
	return nil
}

// readPatch parses a patch file. From, Date and Subject are required.
func readPatch(file string) (*patchFile, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, failure(exitIO, logPatch, err, "Unable to read the patch file %s", filepath.Base(file))
	}
	p, err := parsePatch(toValidUTF8(raw))
	if err != nil {
		return nil, failure(exitUsage, logPatch, err, "Invalid patch file %s", filepath.Base(file))
	}
	return p, nil
}

func parsePatch(text string) (*patchFile, error) {
	p := &patchFile{}
	var diff, body strings.Builder
	hasFrom, hasDate, hasSubject := false, false, false
	section := "headers"
	for _, line := range readLines(text) {
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
				name, email, ok := parseAuthor(v)
				if !ok {
					return nil, fmt.Errorf("invalid From header %q, expected \"Name <email>\"", v)
				}
				p.Name, p.Email, hasFrom = name, email, true
			} else if v, ok := strings.CutPrefix(line, "Date: "); ok {
				date, err := parseDate(strings.TrimSpace(v))
				if err != nil {
					return nil, fmt.Errorf("invalid Date header %q", v)
				}
				p.Date, hasDate = date, true
			} else if v, ok := strings.CutPrefix(line, "Subject: "); ok {
				p.Subject, hasSubject = strings.TrimSpace(v), true
			}
		}
	}
	switch {
	case !hasFrom:
		return nil, errors.New("missing From header")
	case !hasDate:
		return nil, errors.New("missing Date header")
	case !hasSubject:
		return nil, errors.New("missing Subject header")
	}
	p.Diff, p.Body = diff.String(), body.String()
	return p, nil
}

// parseAuthor splits "Name <email>".
func parseAuthor(s string) (name, email string, ok bool) {
	before, rest, found := strings.Cut(s, "<")
	email, after, closed := strings.Cut(rest, ">")
	if !found || !closed || strings.TrimSpace(after) != "" || strings.ContainsAny(email, "<>") {
		return "", "", false
	}
	return strings.TrimSpace(before), strings.TrimSpace(email), true
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
