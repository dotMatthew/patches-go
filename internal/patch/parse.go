// Package patch parses git style patches and applies them to a working tree
// with the same rules as JGit's ApplyCommand (hunks are searched around their
// recorded position, CRLF files are kept CRLF, ...).
package patch

import (
	"fmt"
	"strconv"
	"strings"
)

// ChangeType of a file in a patch.
type ChangeType int

const (
	Modify ChangeType = iota
	Add
	Delete
	Rename
	Copy
)

const devNull = "/dev/null"

// File is one "diff --git" section of a patch.
type File struct {
	OldPath, NewPath string
	OldMode, NewMode int // -1 = not specified
	Type             ChangeType
	Binary           bool // "Binary files differ": nothing can be applied
	Hunks            []*Hunk
}

// Hunk is one "@@ -a,b +c,d @@" block.
type Hunk struct {
	Header                     string
	OldCount, NewStart, NewCnt int
	Context, Deleted, Added    int
	Lines                      []string // body lines without trailing LF
}

func (h *Hunk) String() string { return h.Header }

// Parse parses the patch text. Like JGit, every inconsistency is an error.
func Parse(text string) ([]*File, error) {
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	var files []*File
	var errs []string
	for i := 0; i < len(lines); {
		l := lines[i]
		switch {
		case isHunkHeader(l):
			errs = append(errs, "hunk disconnected from file")
			i++
		case strings.HasPrefix(l, "diff --git "):
			if i == len(lines)-1 {
				i++ // a header as the very last line is ignored (JGit)
				continue
			}
			f := &File{OldMode: -1, NewMode: -1}
			f.OldPath, f.NewPath = parseGitFileName(l[len("diff --git "):])
			i = f.parseHeaders(lines, i+1)
			i = f.parseHunks(lines, i, &errs)
			files = append(files, f)
		case strings.HasPrefix(l, "diff --cc "), strings.HasPrefix(l, "diff --combined "):
			errs = append(errs, "combined diffs are not supported")
			i++
		default:
			i++
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid patch: %s", strings.Join(errs, "; "))
	}
	return files, nil
}

func isHunkHeader(l string) bool { return strings.HasPrefix(l, "@@ -") }

// parseGitFileName splits "a/x b/x" (equal names only, like JGit).
func parseGitFileName(s string) (string, string) {
	aStart := strings.IndexByte(s, '/') + 1
	if aStart == 0 {
		return "", ""
	}
	for sp := 0; sp < len(s); sp++ {
		if s[sp] != ' ' || sp < aStart {
			continue
		}
		slash := strings.IndexByte(s[sp+1:], '/')
		if slash < 0 {
			return "", ""
		}
		if s[aStart:sp] == s[sp+1+slash+1:] {
			if s[0] == '"' {
				if s[sp-1] != '"' {
					return "", ""
				}
				p := p1(Dequote(s[:sp]))
				return p, p
			}
			return s[aStart:sp], s[aStart:sp]
		}
	}
	return "", ""
}

func p1(r string) string {
	if s := strings.IndexByte(r, '/'); s > 0 {
		return r[s+1:]
	}
	return r
}

func parseName(s string) string {
	if strings.HasPrefix(s, "\"") {
		return Dequote(s)
	}
	if i := strings.LastIndexByte(s, '\t'); i >= 0 {
		return s[:i]
	}
	return s
}

func parseMode(s string) int {
	bits := 0
	for _, c := range s {
		bits = bits<<3 + int(c-'0')
	}
	switch bits & 0o170000 {
	case 0o100000:
		if bits&0o111 != 0 {
			return 0o100755
		}
		return 0o100644
	case 0o040000, 0o120000, 0o160000:
		return bits & 0o170000
	}
	return bits
}

func (f *File) parseHeaders(lines []string, i int) int {
	for ; i < len(lines); i++ {
		l := lines[i]
		cut := func(prefix string) (string, bool) { return strings.CutPrefix(l, prefix) }
		if isHunkHeader(l) {
			return i
		} else if v, ok := cut("--- "); ok {
			if f.OldPath = p1(parseName(v)); f.OldPath == devNull {
				f.Type = Add
			}
		} else if v, ok := cut("+++ "); ok {
			if f.NewPath = p1(parseName(v)); f.NewPath == devNull {
				f.Type = Delete
			}
		} else if v, ok := cut("old mode "); ok {
			f.OldMode = parseMode(v)
		} else if v, ok := cut("new mode "); ok {
			f.NewMode = parseMode(v)
		} else if v, ok := cut("deleted file mode "); ok {
			f.OldMode, f.NewMode, f.Type = parseMode(v), 0, Delete
		} else if v, ok := cut("new file mode "); ok {
			f.OldMode, f.NewMode, f.Type = 0, parseMode(v), Add
		} else if v, ok := cut("copy from "); ok {
			f.OldPath, f.Type = parseName(v), Copy
		} else if v, ok := cut("copy to "); ok {
			f.NewPath, f.Type = parseName(v), Copy
		} else if v, ok := firstCut(l, "rename from ", "rename old "); ok {
			f.OldPath, f.Type = parseName(v), Rename
		} else if v, ok := firstCut(l, "rename to ", "rename new "); ok {
			f.NewPath, f.Type = parseName(v), Rename
		} else if _, ok := firstCut(l, "similarity index ", "dissimilarity index "); ok {
			// irrelevant for applying
		} else if v, ok := cut("index "); ok {
			if sp := strings.IndexByte(v, ' '); sp >= 0 {
				f.OldMode = parseMode(v[sp+1:])
				f.NewMode = f.OldMode
			}
		} else {
			return i
		}
	}
	return i
}

func firstCut(l string, prefixes ...string) (string, bool) {
	for _, p := range prefixes {
		if v, ok := strings.CutPrefix(l, p); ok {
			return v, true
		}
	}
	return "", false
}

func (f *File) parseHunks(lines []string, i int, errs *[]string) int {
	for i < len(lines) {
		l := lines[i]
		if strings.HasPrefix(l, "diff --") || strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
			break
		}
		if isHunkHeader(l) {
			h, next, err := parseHunk(lines, i)
			if err != nil {
				*errs = append(*errs, err.Error())
			}
			f.Hunks = append(f.Hunks, h)
			if i = next; i < len(lines) {
				if t := lines[i]; !strings.HasPrefix(t, "@") && !strings.HasPrefix(t, "d") && t != "-- " {
					*errs = append(*errs, "unexpected hunk trailer")
				}
			}
			continue
		}
		if len(f.Hunks) == 0 {
			if strings.HasPrefix(l, "GIT binary patch") {
				*errs = append(*errs, "GIT binary patches are not supported ("+f.NewPath+")")
				for i++; i < len(lines) && !strings.HasPrefix(lines[i], "diff --git "); i++ {
				}
				return i
			}
			if strings.HasSuffix(l, " differ") && (strings.HasPrefix(l, "Binary files ") || strings.HasPrefix(l, "Files ")) {
				f.Binary = true
				return i + 1
			}
		}
		i++
	}
	if len(f.Hunks) == 0 && f.Type == Modify && f.OldMode == f.NewMode {
		f.Binary = true // nothing that could be applied
	}
	return i
}

// parseHunk parses "@@ -oldStart[,oldCount] +newStart[,newCount] @@" and
// its body. The body ends at the first line that is not ' ', '-', '+', '\'
// or empty, regardless of the counts (like JGit); mismatches are errors.
func parseHunk(lines []string, i int) (*Hunk, int, error) {
	h := &Hunk{Header: lines[i], OldCount: 1, NewCnt: 1}
	rest := strings.TrimPrefix(lines[i], "@@ -")
	old, rest, _ := strings.Cut(rest, " ")
	nw, _, _ := strings.Cut(strings.TrimPrefix(rest, "+"), " ")
	if _, c, ok := strings.Cut(old, ","); ok {
		h.OldCount, _ = strconv.Atoi(c)
	}
	s, c, ok := strings.Cut(nw, ",")
	h.NewStart, _ = strconv.Atoi(s)
	if ok {
		h.NewCnt, _ = strconv.Atoi(c)
	}

	start := i + 1
	for i = start; i < len(lines); i++ {
		l := lines[i]
		if l == "" || l[0] == ' ' {
			h.Context++
		} else if l[0] == '-' {
			h.Deleted++
		} else if l[0] == '+' {
			h.Added++
		} else if l[0] != '\\' {
			break
		}
	}
	end := i
	var err error
	switch {
	case end > start && h.Context+h.Deleted-1 == h.OldCount && h.Context+h.Added == h.NewCnt && lines[end-1] == "-- ":
		// "-- " signature footer after the last hunk
		h.Deleted--
		end--
	case h.Context+h.Deleted < h.OldCount:
		err = fmt.Errorf("truncated hunk, at least %d old lines is missing", h.OldCount-h.Context-h.Deleted)
	case h.Context+h.Added < h.NewCnt:
		err = fmt.Errorf("truncated hunk, at least %d new lines is missing", h.NewCnt-h.Context-h.Added)
	case h.Context+h.Deleted > h.OldCount || h.Context+h.Added > h.NewCnt:
		err = fmt.Errorf("hunk header %d:%d does not match body line count of %d:%d",
			h.OldCount, h.NewCnt, h.Context+h.Deleted, h.Context+h.Added)
	}
	h.Lines = lines[start:end]
	return h, end, err
}

// Dequote decodes a C style quoted git path ("a\303\274" etc.).
func Dequote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	in := s[1 : len(s)-1]
	var r []byte
	esc := map[byte]byte{'a': 7, 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': 11, '\\': '\\', '"': '"'}
	for i := 0; i < len(in); i++ {
		if in[i] != '\\' || i+1 == len(in) {
			r = append(r, in[i])
			continue
		}
		i++
		c := in[i]
		if e, ok := esc[c]; ok {
			r = append(r, e)
		} else if c >= '0' && c <= '3' {
			v := int(c - '0')
			for k := 0; k < 2 && i+1 < len(in) && in[i+1] >= '0' && in[i+1] <= '7'; k++ {
				i++
				v = v<<3 | int(in[i]-'0')
			}
			r = append(r, byte(v))
		} else {
			r = append(r, '\\', c)
		}
	}
	return string(r)
}
