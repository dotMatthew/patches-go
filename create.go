package main

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dotMatthew/patches-go/internal/diff"
)

// createPatch writes the HEAD commit of the work directory as patch file.
func (a *app) createPatch(c *config, name string) error {
	a.info(logGit, "Starting to create a patch")
	repo, wt, err := a.openWorktree()
	var clean bool
	if err == nil {
		clean, err = isClean(wt)
	}
	if err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at creating this patch")
	}
	if !clean {
		return failure(exitUsage, logGit, nil, "Your repository is not in a clean state. Cannot create a patch from an unclean state")
	}
	head, err := commitOf(repo, "HEAD")
	if err != nil {
		return crash("%v", err)
	}
	parent, err := commitOf(repo, "HEAD~1")
	if err != nil {
		return crash("%v", err)
	}
	diffText, err := a.formatDiff(repo, parent, head)
	if err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at creating this patch")
	}
	subject, body, ok := strings.Cut(head.Message, "\n")
	if !ok || strings.TrimSpace(head.Message) == "" {
		return crash("commit message has no subject line")
	}
	file := filepath.Join(a.path(c.PatchesDir), name+".patch")
	// Like in the Java version a failed write is only logged.
	if err := writePatch(file, diffText, subject, body, head.Author.Name, head.Author.Email, head.Author.When); err != nil {
		a.report(err)
	}
	a.info(logGit, "Patch successfully created and saved to disk! (%s.patch)", name)
	return nil
}

func commitOf(repo *git.Repository, rev string) (*object.Commit, error) {
	h, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", rev, err)
	}
	return repo.CommitObject(*h)
}

// formatDiff renders the changes between two commits like JGit's DiffFormatter.
func (a *app) formatDiff(repo *git.Repository, from, to *object.Commit) (string, error) {
	entries, err := diffEntries(from, to)
	if err != nil {
		return "", err
	}
	f := diff.NewFormatter()
	if cfg, err := repo.ConfigScoped(gitconfig.SystemScope); err == nil && cfg.Raw != nil {
		if isTrue(cfg.Raw.Section("diff").Option("noprefix"), false) {
			f.OldPrefix, f.NewPrefix = "", ""
		}
		f.QuotePaths = isTrue(cfg.Raw.Section("core").Option("quotepath"), true)
		if strings.EqualFold(cfg.Raw.Section("diff").Option("algorithm"), "myers") {
			f.Algorithm = diff.Myers
		}
	}
	f.LoadBlob = func(id string) ([]byte, error) {
		blob, err := repo.BlobObject(plumbing.NewHash(id))
		if err != nil {
			return nil, err
		}
		r, err := blob.Reader()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	f.Abbreviate = abbreviator(filepath.Join(a.workdir(), ".git", "objects"))
	var out bytes.Buffer
	for _, e := range entries {
		if err := f.Format(&out, e); err != nil {
			return "", err
		}
	}
	return toValidUTF8(out.Bytes()), nil
}

// isTrue parses a git config boolean.
func isTrue(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	default:
		return def
	}
}

type treeFile struct {
	mode uint32
	id   string
}

// treeFiles lists all files (including submodules) of a commit.
func treeFiles(c *object.Commit) (map[string]treeFile, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	files := map[string]treeFile{}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, e, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		} else if err != nil {
			return nil, err
		}
		if e.Mode != filemode.Dir {
			files[name] = treeFile{uint32(e.Mode), e.Hash.String()}
		}
	}
}

// diffEntries lists the changed files in path order (no rename detection).
func diffEntries(from, to *object.Commit) ([]*diff.Entry, error) {
	a, err := treeFiles(from)
	if err != nil {
		return nil, err
	}
	b, err := treeFiles(to)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(a)+len(b))
	for p := range a {
		paths = append(paths, p)
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)

	deleted := func(p string, o treeFile) *diff.Entry {
		return &diff.Entry{Type: diff.Delete, OldPath: p, NewPath: "/dev/null", OldMode: o.mode, OldID: o.id, NewID: diff.ZeroID}
	}
	added := func(p string, n treeFile) *diff.Entry {
		return &diff.Entry{Type: diff.Add, OldPath: "/dev/null", NewPath: p, NewMode: n.mode, OldID: diff.ZeroID, NewID: n.id}
	}
	var entries []*diff.Entry
	for _, p := range paths {
		o, inA := a[p]
		n, inB := b[p]
		switch {
		case !inB:
			entries = append(entries, deleted(p, o))
		case !inA:
			entries = append(entries, added(p, n))
		case o == n:
			// unchanged
		case diff.SameType(o.mode, n.mode):
			entries = append(entries, &diff.Entry{Type: diff.Modify, OldPath: p, NewPath: p, OldMode: o.mode, NewMode: n.mode, OldID: o.id, NewID: n.id})
		default: // a type change (e.g. file -> symlink) is a delete plus an add
			entries = append(entries, deleted(p, o), added(p, n))
		}
	}
	return entries, nil
}

// abbreviator returns a function that abbreviates object ids like git: at
// least 7 characters, longer if the prefix is ambiguous in the repository.
// The object ids are loaded on first use.
func abbreviator(objectsDir string) func(string) string {
	var ids []string
	loaded := false
	return func(id string) string {
		if !loaded {
			ids, loaded = objectIDs(objectsDir), true
		}
		l := 7
		for l < len(id) && ambiguous(ids, id[:l]) {
			l++
		}
		return id[:l]
	}
}

// ambiguous reports whether more than one id in the sorted list has prefix.
func ambiguous(ids []string, prefix string) bool {
	i, _ := slices.BinarySearch(ids, prefix)
	return i+1 < len(ids) && strings.HasPrefix(ids[i], prefix) && strings.HasPrefix(ids[i+1], prefix)
}

// objectIDs returns the sorted, unique ids of all loose and packed objects.
func objectIDs(objectsDir string) []string {
	var ids []string
	loose, _ := filepath.Glob(filepath.Join(objectsDir, "[0-9a-f][0-9a-f]", "*"))
	for _, p := range loose {
		ids = append(ids, filepath.Base(filepath.Dir(p))+filepath.Base(p))
	}
	packs, _ := filepath.Glob(filepath.Join(objectsDir, "pack", "*.idx"))
	for _, p := range packs {
		ids = append(ids, packedIDs(p)...)
	}
	slices.SortFunc(ids, cmp.Compare)
	return slices.Compact(ids)
}

// packedIDs reads the object ids of a pack index; unreadable indexes are
// skipped (the abbreviation is then just possibly too short).
func packedIDs(idxPath string) []string {
	f, err := os.Open(idxPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	idx := idxfile.NewMemoryIndex()
	if idxfile.NewDecoder(f).Decode(idx) != nil {
		return nil
	}
	it, err := idx.Entries()
	if err != nil {
		return nil
	}
	defer it.Close()
	var ids []string
	for e, err := it.Next(); err == nil; e, err = it.Next() {
		ids = append(ids, hex.EncodeToString(e.Hash[:]))
	}
	return ids
}
