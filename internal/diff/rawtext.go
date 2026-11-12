// Package diff is a port of the parts of JGit's org.eclipse.jgit.diff package
// that are needed to produce byte-identical patches: RawText, the default
// RawTextComparator, HistogramDiff (with MyersDiff fallback) and the unified
// output of DiffFormatter.
//
// JGit is licensed under the Eclipse Distribution License v1.0 (BSD-3-Clause).
package diff

import "bytes"

// RawText is a sequence of lines (JGit RawText).
type RawText struct {
	Content []byte
	// lines mirrors JGit's line map: lines[0] is a sentinel, lines[i+1] is
	// the start of line i and the last entry is len(Content).
	lines []int
	// hashes of every line (including the trailing LF), lazily computed.
	hashes []int32
}

// NewRawText builds a RawText from content.
func NewRawText(content []byte) *RawText {
	lines := make([]int, 0, len(content)/36+2)
	lines = append(lines, -1<<31)
	for ptr := 0; ptr < len(content); {
		lines = append(lines, ptr)
		nl := bytes.IndexByte(content[ptr:], '\n')
		if nl < 0 {
			ptr = len(content)
		} else {
			ptr += nl + 1
		}
	}
	lines = append(lines, len(content))
	return &RawText{Content: content, lines: lines}
}

// Size is the number of lines.
func (t *RawText) Size() int { return len(t.lines) - 2 }

func (t *RawText) start(i int) int { return t.lines[i+1] }
func (t *RawText) end(i int) int   { return t.lines[i+2] }

// Line returns line i without its trailing LF.
func (t *RawText) Line(i int) []byte {
	s, e := t.start(i), t.end(i)
	if e > s && t.Content[e-1] == '\n' {
		e--
	}
	return t.Content[s:e]
}

// IsMissingNewlineAtEnd reports whether the last line lacks a LF.
func (t *RawText) IsMissingNewlineAtEnd() bool {
	end := t.lines[len(t.lines)-1]
	if end == 0 {
		return true
	}
	return t.Content[end-1] != '\n'
}

func (t *RawText) hash(i int) int32 {
	if t.hashes == nil {
		t.hashes = make([]int32, t.Size())
		for k := range t.hashes {
			h := int32(5381)
			for _, b := range t.Content[t.start(k):t.end(k)] {
				h = (h << 5) + h + int32(b)
			}
			t.hashes[k] = h
		}
	}
	return t.hashes[i]
}

// equals is RawTextComparator.DEFAULT.equals (compares including LF).
func equals(a *RawText, ai int, b *RawText, bi int) bool {
	return bytes.Equal(a.Content[a.start(ai):a.end(ai)], b.Content[b.start(bi):b.end(bi)])
}

// hashedEquals is HashedSequenceComparator.equals.
func hashedEquals(a *RawText, ai int, b *RawText, bi int) bool {
	return a.hash(ai) == b.hash(bi) && equals(a, ai, b, bi)
}

// BufferSize is RawText.getBufferSize() (FIRST_FEW_BYTES).
const BufferSize = 8 * 1024

// BinaryFileThreshold is PackConfig.DEFAULT_BIG_FILE_THRESHOLD.
const BinaryFileThreshold = 50 * 1024 * 1024

func isBinaryByte(curr, prev byte) bool {
	return curr == 0 || (curr != '\n' && prev == '\r') || prev == 0
}

// IsBinary mirrors RawText.load(loader, threshold) throwing BinaryBlobException.
func IsBinary(data []byte) bool {
	if len(data) > BinaryFileThreshold {
		return true
	}
	if len(data) <= BufferSize {
		last := byte('x')
		for _, c := range data {
			if isBinaryByte(c, last) {
				return true
			}
			last = c
		}
		return last == '\r'
	}
	last := byte('x')
	for _, c := range data[:BufferSize] {
		if isBinaryByte(c, last) {
			return true
		}
		last = c
	}
	return false
}

// IsCrLfText mirrors RawText.isCrLfText(InputStream).
func IsCrLfText(raw []byte) bool {
	// The stream variant only looks at the first BufferSize bytes and never
	// treats the input as complete.
	if len(raw) > BufferSize {
		raw = raw[:BufferSize]
	}
	hasCrLf := false
	last := byte('x')
	for _, c := range raw {
		if isBinaryByte(c, last) {
			return false
		}
		if c == '\n' && last == '\r' {
			hasCrLf = true
		}
		last = c
	}
	return hasCrLf
}
