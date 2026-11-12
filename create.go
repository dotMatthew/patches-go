package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dotMatthew/patches-go/internal/diff"
)

func createPatch(c *config, name string) int {
	logGit.info("Starting to create a patch")
	repo, wt, err := openWorktree()
	var clean bool
	if err == nil {
		clean, err = isClean(wt)
	}
	if err != nil {
		logGit.fail(err, "An error occurred at creating this patch")
		return exitInternal
	}
	if !clean {
		logGit.error("Your repository is not in a clean state. Cannot create a patch from an unclean state")
		return exitUsage
	}
	head, err := commitOf(repo, "HEAD")
	if err != nil {
		return crash(err.Error())
	}
	parent, err := commitOf(repo, "HEAD~1")
	if err != nil {
		return crash(err.Error())
	}
	diffText, err := formatDiff(repo, parent, head)
	if err != nil {
		logGit.fail(err, "An error occurred at creating this patch")
		return exitInternal
	}
	subject, body, ok := strings.Cut(head.Message, "\n")
	if !ok || strings.TrimSpace(head.Message) == "" {
		return crash("commit message has no subject line")
	}
	// Like the Java version a write error is only logged.
	writePatch(c.PatchesDir+"/"+name+".patch", diffText, subject, body, head.Author.Name, head.Author.Email, head.Author.When)
	logGit.info("Patch successfully created and saved to disk! (%s.patch)", name)
	return exitOK
}

func commitOf(repo *git.Repository, rev string) (*object.Commit, error) {
	h, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", rev, err)
	}
	return repo.CommitObject(*h)
}

// formatDiff renders the changes between two commits like JGit's DiffFormatter.
func formatDiff(repo *git.Repository, from, to *object.Commit) (string, error) {
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
	f.Abbreviate = abbreviator(filepath.Join(workdir, ".git", "objects"))
	var out bytes.Buffer
	for _, e := range entries {
		if err := f.Format(&out, e); err != nil {
			return "", err
		}
	}
	return toValidUTF8(out.Bytes()), nil
}

func isTrue(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	}
	return def
}

type treeFile struct {
	mode uint32
	id   string
}

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
	var paths []string
	for p := range a {
		paths = append(paths, p)
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
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
		case diff.SameType(o.mode, n.mode):
			entries = append(entries, &diff.Entry{Type: diff.Modify, OldPath: p, NewPath: p, OldMode: o.mode, NewMode: n.mode, OldID: o.id, NewID: n.id})
		default: // a type change (e.g. file -> symlink) is a delete plus an add
			entries = append(entries, deleted(p, o), added(p, n))
		}
	}
	return entries, nil
}

// abbreviator returns the shortest unique abbreviation (at least 7 chars)
// of an object id among all objects of the repository.
func abbreviator(objectsDir string) func(string) string {
	var ids []string
	loaded := false
	load := func() {
		loaded = true
		loose, _ := filepath.Glob(filepath.Join(objectsDir, "[0-9a-f][0-9a-f]", "*"))
		for _, p := range loose {
			ids = append(ids, filepath.Base(filepath.Dir(p))+filepath.Base(p))
		}
		packs, _ := filepath.Glob(filepath.Join(objectsDir, "pack", "*.idx"))
		for _, p := range packs {
			f, err := os.Open(p)
			if err != nil {
				continue
			}
			idx := idxfile.NewMemoryIndex()
			if idxfile.NewDecoder(f).Decode(idx) == nil {
				if it, err := idx.Entries(); err == nil {
					for e, err := it.Next(); err == nil; e, err = it.Next() {
						ids = append(ids, hex.EncodeToString(e.Hash[:]))
					}
				}
			}
			f.Close()
		}
		sort.Strings(ids)
	}
	ambiguous := func(prefix string) bool {
		i := sort.SearchStrings(ids, prefix)
		for j := i + 1; j < len(ids) && strings.HasPrefix(ids[j], prefix); j++ {
			if ids[j] != ids[i] {
				return true
			}
		}
		return false
	}
	return func(id string) string {
		if !loaded {
			load()
		}
		l := 7
		for l < len(id) && ambiguous(id[:l]) {
			l++
		}
		return id[:l]
	}
}
