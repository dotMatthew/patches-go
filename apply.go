package main

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dotMatthew/patches-go/internal/patch"
)

// applyPatches applies all patches to the work directory, one commit each.
func (a *app) applyPatches(c *config) error {
	files := listPatches(a.path(c.PatchesDir))
	if len(files) == 0 {
		a.error(logGit, "No patches to apply found")
		return nil
	}
	_, wt, err := a.openWorktree()
	var clean bool
	if err == nil {
		clean, err = isClean(wt)
	}
	if err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at applying patch")
	}
	if !clean {
		return failure(exitUsage, logGit, nil, "The repository is not in a clean state. To apply patches you need a clean repository!")
	}
	for _, file := range files {
		if err := a.applyPatch(wt, file); err != nil {
			return err
		}
	}
	return nil
}

// applyPatch applies one patch file and commits it with the author, date
// and message of the patch.
func (a *app) applyPatch(wt *git.Worktree, file string) error {
	name := filepath.Base(file)
	a.info(logGit, "Applying patch %s", name)
	p, err := readPatch(file)
	if err != nil {
		a.report(err)
		return failure(exitInternal, logGit, nil, "An error occurred at applying patch")
	}
	if err := a.applyAndStage(wt, p.Diff); err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at applying patch %s ", name)
	}
	// Like in the Java version the author is checked after the diff has
	// been applied (the changes then stay staged).
	if !p.HasAuthor || p.Date.IsZero() {
		return failure(exitInternal, logGit, nil, "An error occurred at applying patch")
	}
	sig := &object.Signature{Name: p.Name, Email: p.Email, When: p.Date}
	opts := &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}
	if _, err := wt.Commit(p.Subject+"\n\n"+p.Body, opts); err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at applying patch %s ", name)
	}
	a.info(logGit, "Applied patch %s successfully as commit", name)
	return nil
}

// applyAndStage applies the diff and stages all changes; patched files are
// staged even if they are ignored.
func (a *app) applyAndStage(wt *git.Worktree, diffText string) error {
	written, err := patch.Apply(a.workdir(), diffText)
	if err != nil {
		return err
	}
	for _, p := range written {
		if _, err := wt.Add(p); err != nil {
			return err
		}
	}
	return wt.AddWithOptions(&git.AddOptions{All: true})
}

// listPatches returns the *.patch files of dir: names starting with a number
// first, ordered by that number ("00001_a" < "002_b" < "10_c"), then the
// other names alphabetically.
func listPatches(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".patch") {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, comparePatchNames)
	files := make([]string, len(names))
	for i, n := range names {
		files[i] = filepath.Join(dir, n)
	}
	return files
}

// comparePatchNames orders by the leading number (any length, leading zeros
// ignored), names without a number last, then by name.
func comparePatchNames(a, b string) int {
	numA, okA := leadingNumber(a)
	numB, okB := leadingNumber(b)
	switch {
	case okA != okB:
		if okA {
			return -1
		}
		return 1
	case okA && numA != numB:
		// compare as numbers without overflow: shorter is smaller
		return cmp.Or(cmp.Compare(len(numA), len(numB)), cmp.Compare(numA, numB))
	default:
		return cmp.Compare(a, b)
	}
}

// leadingNumber returns the leading digits of name without leading zeros.
func leadingNumber(name string) (string, bool) {
	rest := strings.TrimLeft(name, "0123456789")
	digits := name[:len(name)-len(rest)]
	return strings.TrimLeft(digits, "0"), digits != ""
}

// convertPatches writes the plain git diff of every patch to ./converted.
func (a *app) convertPatches(c *config) error {
	files := listPatches(a.path(c.PatchesDir))
	if len(files) == 0 {
		a.error(logGit, "No patches to convert found")
		return nil
	}
	out := a.path("converted")
	if err := os.MkdirAll(out, 0o777); err != nil {
		return failure(exitCrash, logGit, err, "Unable to create converted directory")
	}
	for _, file := range files {
		name := filepath.Base(file)
		target := filepath.Join(out, name)
		p, err := readPatch(file)
		if err != nil {
			a.report(err)
			// The Java version creates the (empty) file before it fails.
			_ = os.WriteFile(target, nil, 0o644)
			return crash("invalid patch %s", name)
		}
		if err := os.WriteFile(target, []byte(p.Diff), 0o644); err != nil {
			return failure(exitCrash, logGit, err, "Unable to write converted patches to disk")
		}
		a.info(logGit, "Successfully converted patch %s", name)
	}
	return nil
}
