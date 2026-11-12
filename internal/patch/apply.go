package patch

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dotMatthew/patches-go/internal/diff"
)

// Apply applies the patch to the working tree rooted at root. It returns the
// paths whose content was written (they must be staged even if ignored).
func Apply(root, text string) ([]string, error) {
	files, err := Parse(text)
	if err != nil {
		return nil, err
	}
	path := func(p string) string { return filepath.Join(root, filepath.FromSlash(p)) }
	var written []string
	for _, f := range files {
		var err error
		switch f.Type {
		case Add:
			err = createEmpty(path(f.NewPath))
			if err == nil {
				err = applyFile(path(f.NewPath), f)
			}
		case Modify:
			err = applyFile(path(f.OldPath), f)
		case Delete:
			if os.Remove(path(f.OldPath)) != nil {
				err = fmt.Errorf("cannot delete file %s", path(f.OldPath))
			}
		case Rename, Copy:
			src, dest := path(f.OldPath), path(f.NewPath)
			if err = os.MkdirAll(filepath.Dir(dest), 0o777); err == nil {
				if f.Type == Rename {
					err = os.Rename(src, dest)
				} else {
					err = copyFile(src, dest)
				}
			}
			if err == nil {
				err = applyFile(dest, f)
			}
		}
		if err != nil {
			return nil, err
		}
		if f.Type != Delete && !f.Binary {
			written = append(written, f.NewPath)
		}
	}
	return written, nil
}

func createEmpty(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return fmt.Errorf("cannot create new file %s: %w", p, err)
	}
	return f.Close()
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func applyFile(p string, f *File) error {
	if f.Binary {
		return nil // "Binary files differ" carries no content
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("cannot apply: %w", err)
	}
	// A CRLF file patched with a LF patch is converted to LF and back.
	crlf := !hunksHaveCRLF(f) && diff.IsCrLfText(data)
	if crlf {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	out, err := applyHunks(diff.NewRawText(data), f.Hunks)
	if err != nil {
		return err
	}
	if crlf {
		out = toCRLF(out)
	}
	// The file is replaced (never written through a symlink).
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// new files get their permissions from the umask, like in JGit
	perm := os.FileMode(0o666)
	if f.NewMode == 0o100755 {
		perm = 0o777
	}
	return os.WriteFile(p, out, perm)
}

func hunksHaveCRLF(f *File) bool {
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if len(l) > 1 && (l[0] == ' ' || l[0] == '-') && l[len(l)-1] == '\r' {
				return true
			}
		}
	}
	return false
}

// applyHunks applies the hunks to the lines of rt (JGit PatchApplier.applyText).
func applyHunks(rt *diff.RawText, hunks []*Hunk) ([]byte, error) {
	lines := make([]string, rt.Size())
	for i := range lines {
		lines[i] = string(rt.Line(i))
	}
	afterLastHunk, shift, lastNewStart := 0, 0, -1
	lastWasRemoval, noNewlineAtEnd := false, false
	for _, h := range hunks {
		if h.NewStart <= lastNewStart {
			return nil, fmt.Errorf("cannot apply %s", h)
		}
		lastNewStart = h.NewStart
		if h.NewStart == 0 { // must be the single hunk clearing the file
			if len(hunks) == 1 && matches(h, lines, 0) {
				lines = nil
				break
			}
			return nil, fmt.Errorf("cannot apply %s", h)
		}
		at, ok := findPosition(h, lines, h.NewStart-1, &shift, afterLastHunk)
		if !ok {
			return nil, fmt.Errorf("cannot apply %s", h)
		}
		shift = at - h.NewStart + 1
		for _, l := range h.Lines {
			switch {
			case l == "" || l[0] == ' ':
				at++
				lastWasRemoval = false
			case l[0] == '-':
				lines = append(lines[:at], lines[at+1:]...)
				lastWasRemoval = true
			case l[0] == '+':
				lines = append(lines[:at], append([]string{l[1:]}, lines[at:]...)...)
				at++
				lastWasRemoval = false
			case l == `\ No newline at end of file` && !lastWasRemoval:
				noNewlineAtEnd = true
			}
		}
		afterLastHunk = at
	}
	finalNewline := !rt.IsMissingNewlineAtEnd()
	if lastNewStart >= 0 && afterLastHunk == len(lines) {
		finalNewline = !noNewlineAtEnd
	}
	var out bytes.Buffer
	for i, l := range lines {
		out.WriteString(l)
		if i < len(lines)-1 || finalNewline {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// findPosition searches the position of a hunk: first at the expected line
// (corrected by the shift of previous hunks), then upwards, then downwards.
func findPosition(h *Hunk, lines []string, start int, shift *int, minPos int) (int, bool) {
	at := start + *shift
	if at < minPos && *shift < 0 {
		at, *shift = start, 0
	}
	if at < minPos {
		return 0, false
	}
	oldLines := h.Context + h.Deleted
	if oldLines <= 1 { // no context: do not move the hunk around
		if matches(h, lines, at) {
			return at, true
		}
		return start, *shift != 0 && start >= minPos && matches(h, lines, start)
	}
	for pos := at; pos >= minPos; pos-- {
		if matches(h, lines, pos) {
			return pos, true
		}
	}
	for pos := at + 1; pos <= len(lines)-oldLines; pos++ {
		if matches(h, lines, pos) {
			return pos, true
		}
	}
	return 0, false
}

// matches reports whether the context and removed lines of h are at pos.
func matches(h *Hunk, lines []string, pos int) bool {
	for _, l := range h.Lines {
		if l != "" && l[0] != ' ' && l[0] != '-' {
			continue
		}
		want := ""
		if l != "" {
			want = l[1:]
		}
		if pos >= len(lines) || lines[pos] != want {
			return false
		}
		pos++
	}
	return true
}

// toCRLF turns every LF that is not preceded by CR into CRLF.
func toCRLF(in []byte) []byte {
	out := make([]byte, 0, len(in)+len(in)/16)
	for i, c := range in {
		if c == '\n' && (i == 0 || in[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, c)
	}
	return out
}
