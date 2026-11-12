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
	Subject     *string // nil if the header is missing
	Name, Email *string
	Date        *time.Time
}

const rfc1123 = "Mon, 2 Jan 2006 15:04:05 -0700"

// writePatch writes a patch file; problems are logged and returned as exit code.
func writePatch(file, diffText, subject, body, name, email string, date time.Time) int {
	switch {
	case strings.TrimSpace(diffText) == "":
		logPatch.error("No diff text found in commit!")
		return exitInternal
	case !strings.HasPrefix(diffText, "diff --git "):
		logPatch.error("The specified diff is incorrect!")
		return exitInternal
	case strings.TrimSpace(name) == "":
		logPatch.error("The name of the author is null or blank!")
		return exitInternal
	case strings.TrimSpace(email) == "":
		logPatch.error("The email of the author is null or blank!")
		return exitInternal
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
		logPatch.fail(err, "Unable to write patch file to disk")
		return exitOSError
	}
	return exitOK
}

// readPatch parses a patch file; nil means invalid (the reason is logged).
func readPatch(file string) *patchFile {
	raw, err := os.ReadFile(file)
	if err != nil {
		logPatch.fail(err, "Unable to read patch files from disk")
		return nil
	}
	p := &patchFile{}
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
				parts := strings.Split(v, "<")
				for len(parts) > 0 && parts[len(parts)-1] == "" {
					parts = parts[:len(parts)-1]
				}
				if len(parts) != 2 {
					logPatch.error("Unable to parse from line from patch")
					return nil
				}
				name := strings.TrimSpace(parts[0])
				email := strings.ReplaceAll(strings.TrimSpace(parts[1]), ">", "")
				p.Name, p.Email = &name, &email
			} else if v, ok := strings.CutPrefix(line, "Date: "); ok {
				date, err := parseDate(strings.TrimSpace(v))
				if err != nil {
					logPatch.error("Unable to parse date from line from patch")
					return nil
				}
				p.Date = &date
			} else if v, ok := strings.CutPrefix(line, "Subject: "); ok {
				subject := strings.TrimSpace(v)
				p.Subject = &subject
			}
		}
	}
	p.Diff, p.Body = diff.String(), body.String()
	return p
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
