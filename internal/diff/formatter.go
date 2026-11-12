package diff

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// File modes as used by git trees.
const (
	ModeMissing    uint32 = 0
	ModeTree       uint32 = 0o040000
	ModeRegular    uint32 = 0o100644
	ModeExecutable uint32 = 0o100755
	ModeSymlink    uint32 = 0o120000
	ModeGitlink    uint32 = 0o160000
	typeMask       uint32 = 0o170000
)

// ChangeType of a DiffEntry.
type ChangeType int

const (
	Add ChangeType = iota
	Modify
	Delete
)

// Entry is a JGit DiffEntry (without rename/copy support).
type Entry struct {
	Type             ChangeType
	OldPath, NewPath string
	OldMode, NewMode uint32
	OldID, NewID     string // full 40 char hex ids, zero id for missing sides
}

// ZeroID is the all zero object id.
const ZeroID = "0000000000000000000000000000000000000000"

// SameType mirrors RenameDetector.sameType.
func SameType(a, b uint32) bool { return a&typeMask == b&typeMask }

// Formatter mirrors the parts of JGit's DiffFormatter used by the tool.
type Formatter struct {
	OldPrefix, NewPrefix string
	QuotePaths           bool
	Algorithm            Algorithm
	Context              int
	// LoadBlob returns the content of a blob.
	LoadBlob func(id string) ([]byte, error)
	// Abbreviate returns the abbreviated form of an object id.
	Abbreviate func(id string) string
}

// NewFormatter creates a formatter with JGit's defaults.
func NewFormatter() *Formatter {
	return &Formatter{OldPrefix: "a/", NewPrefix: "b/", QuotePaths: true, Algorithm: Histogram, Context: 3,
		Abbreviate: func(id string) string { return id[:7] }}
}

func modeString(m uint32) string { return strconv.FormatUint(uint64(m), 8) }

// Format appends the formatted entry to out.
func (f *Formatter) Format(out *bytes.Buffer, ent *Entry) error {
	f.formatHeader(out, ent)

	var a, b *RawText
	if ent.OldMode == ModeGitlink || ent.NewMode == ModeGitlink {
		a = NewRawText(gitLinkText(ent.OldID))
		b = NewRawText(gitLinkText(ent.NewID))
	} else {
		var binary bool
		var err error
		a, binary, err = f.open(ent.OldMode, ent.OldID)
		if err != nil {
			return err
		}
		if !binary {
			b, binary, err = f.open(ent.NewMode, ent.NewID)
			if err != nil {
				return err
			}
		}
		if binary {
			f.formatOldNewPaths(out, ent)
			out.WriteString("Binary files differ\n")
			return nil
		}
	}
	edits := Diff(f.Algorithm, a, b)
	f.formatOldNewPaths(out, ent)
	f.formatEdits(out, edits, a, b)
	return nil
}

func gitLinkText(id string) []byte {
	if id == ZeroID {
		return nil
	}
	return []byte("Subproject commit " + id + "\n")
}

func (f *Formatter) open(mode uint32, id string) (*RawText, bool, error) {
	if mode == ModeMissing || mode&typeMask == ModeTree || mode == ModeGitlink {
		return NewRawText(nil), false, nil
	}
	data, err := f.LoadBlob(id)
	if err != nil {
		return nil, false, err
	}
	if IsBinary(data) {
		return nil, true, nil
	}
	return NewRawText(data), false, nil
}

func (f *Formatter) quotePath(p string) string {
	return QuotePath(p, f.QuotePaths)
}

func (f *Formatter) formatHeader(o *bytes.Buffer, ent *Entry) {
	oldName := ent.OldPath
	if ent.Type == Add {
		oldName = ent.NewPath
	}
	newName := ent.NewPath
	if ent.Type == Delete {
		newName = ent.OldPath
	}
	o.WriteString("diff --git " + f.quotePath(f.OldPrefix+oldName) + " " + f.quotePath(f.NewPrefix+newName) + "\n")

	if ent.Type == Modify && ent.OldMode != ent.NewMode {
		o.WriteString("old mode " + modeString(ent.OldMode) + "\n")
		o.WriteString("new mode " + modeString(ent.NewMode) + "\n")
	}
	switch ent.Type {
	case Add:
		o.WriteString("new file mode " + modeString(ent.NewMode) + "\n")
	case Delete:
		o.WriteString("deleted file mode " + modeString(ent.OldMode) + "\n")
	}
	if ent.OldID != ent.NewID {
		o.WriteString("index " + f.Abbreviate(ent.OldID) + ".." + f.Abbreviate(ent.NewID))
		if ent.OldMode == ent.NewMode {
			o.WriteString(" " + modeString(ent.NewMode))
		}
		o.WriteString("\n")
	}
}

func (f *Formatter) formatOldNewPaths(o *bytes.Buffer, ent *Entry) {
	if ent.OldID == ent.NewID {
		return
	}
	var oldp, newp string
	switch ent.Type {
	case Add:
		oldp = "/dev/null"
		newp = f.quotePath(f.NewPrefix + ent.NewPath)
	case Delete:
		oldp = f.quotePath(f.OldPrefix + ent.OldPath)
		newp = "/dev/null"
	default:
		oldp = f.quotePath(f.OldPrefix + ent.OldPath)
		newp = f.quotePath(f.NewPrefix + ent.NewPath)
	}
	o.WriteString("--- " + oldp + "\n")
	o.WriteString("+++ " + newp + "\n")
}

const noNewLine = "\\ No newline at end of file\n"

func (f *Formatter) formatEdits(out *bytes.Buffer, edits []*Edit, a, b *RawText) {
	ctx := f.Context
	for curIdx := 0; curIdx < len(edits); {
		curEdit := edits[curIdx]
		endIdx := f.findCombinedEnd(edits, curIdx)
		endEdit := edits[endIdx]

		aCur := max(0, curEdit.BeginA-ctx)
		bCur := max(0, curEdit.BeginB-ctx)
		aEnd := min(a.Size(), endEdit.EndA+ctx)
		bEnd := min(b.Size(), endEdit.EndB+ctx)

		writeHunkHeader(out, aCur, aEnd, bCur, bEnd)

		for aCur < aEnd || bCur < bEnd {
			if aCur < curEdit.BeginA || endIdx+1 < curIdx {
				writeLine(out, ' ', a, aCur)
				aCur++
				bCur++
			} else if aCur < curEdit.EndA {
				writeLine(out, '-', a, aCur)
				aCur++
			} else if bCur < curEdit.EndB {
				writeLine(out, '+', b, bCur)
				bCur++
			}
			if curEdit.EndA <= aCur && curEdit.EndB <= bCur {
				curIdx++
				if curIdx < len(edits) {
					curEdit = edits[curIdx]
				}
			}
		}
	}
}

func writeLine(out *bytes.Buffer, prefix byte, t *RawText, line int) {
	out.WriteByte(prefix)
	out.Write(t.Line(line))
	out.WriteByte('\n')
	if line+1 == t.Size() && t.IsMissingNewlineAtEnd() {
		out.WriteString(noNewLine)
	}
}

func (f *Formatter) findCombinedEnd(edits []*Edit, i int) int {
	end := i + 1
	for end < len(edits) && (edits[end].BeginA-edits[end-1].EndA <= 2*f.Context ||
		edits[end].BeginB-edits[end-1].EndB <= 2*f.Context) {
		end++
	}
	return end - 1
}

func writeHunkHeader(out *bytes.Buffer, aStart, aEnd, bStart, bEnd int) {
	out.WriteString("@@")
	writeRange(out, '-', aStart+1, aEnd-aStart)
	writeRange(out, '+', bStart+1, bEnd-bStart)
	out.WriteString(" @@\n")
}

func writeRange(out *bytes.Buffer, prefix byte, begin, cnt int) {
	out.WriteByte(' ')
	out.WriteByte(prefix)
	switch cnt {
	case 0:
		out.WriteString(strconv.Itoa(begin - 1))
		out.WriteString(",0")
	case 1:
		out.WriteString(strconv.Itoa(begin))
	default:
		out.WriteString(strconv.Itoa(begin))
		out.WriteByte(',')
		out.WriteString(strconv.Itoa(cnt))
	}
}

var pathEscapes = map[byte]byte{'\a': 'a', '\b': 'b', '\f': 'f', '\n': 'n', '\r': 'r', '\t': 't', '\v': 'v', '\\': '\\', '"': '"'}

// QuotePath quotes a path like git (core.quotePath): characters outside of
// [A-Za-z0-9] and " $%&*+,-./:;=?@_^|~" are C-escaped, bytes >= 0x80 only
// if quoteHigh is set.
func QuotePath(in string, quoteHigh bool) string {
	if in == "" {
		return `""`
	}
	var out strings.Builder
	quoted := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c >= 0x80 && !quoteHigh, c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z',
			c < 0x80 && strings.IndexByte(" $%&*+,-./:;=?@_^|~", c) >= 0:
			out.WriteByte(c)
		case pathEscapes[c] != 0:
			quoted = true
			out.WriteByte('\\')
			out.WriteByte(pathEscapes[c])
		default:
			quoted = true
			fmt.Fprintf(&out, "\\%03o", c)
		}
	}
	if !quoted {
		return in
	}
	return `"` + out.String() + `"`
}
