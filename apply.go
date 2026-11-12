package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dotMatthew/patches-go/internal/patch"
)

func applyPatches(c *config) int {
	files := listPatches(c.PatchesDir)
	if len(files) == 0 {
		logGit.error("No patches to apply found")
		return exitOK
	}
	_, wt, err := openWorktree()
	var clean bool
	if err == nil {
		clean, err = isClean(wt)
	}
	if err != nil {
		logGit.fail(err, "An error occurred at applying patch")
		return exitInternal
	}
	if !clean {
		logGit.error("The repository is not in a clean state. To apply patches you need a clean repository!")
		return exitUsage
	}
	for _, file := range files {
		name := filepath.Base(file)
		logGit.info("Applying patch %s", name)
		p := readPatch(file)
		if p == nil {
			logGit.error("An error occurred at applying patch")
			return exitInternal
		}
		if err := applyAndStage(wt, p.Diff); err != nil {
			logGit.fail(err, "An error occurred at applying patch %s ", name)
			return exitInternal
		}
		if p.Name == nil || p.Date == nil {
			logGit.error("An error occurred at applying patch")
			return exitInternal
		}
		subject := "null" // the Java version concatenates the null reference
		if p.Subject != nil {
			subject = *p.Subject
		}
		sig := &object.Signature{Name: *p.Name, Email: *p.Email, When: *p.Date}
		if _, err := wt.Commit(subject+"\n\n"+p.Body, &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}); err != nil {
			logGit.fail(err, "An error occurred at applying patch %s ", name)
			return exitInternal
		}
		logGit.info("Applied patch %s successfully as commit", name)
	}
	return exitOK
}

// applyAndStage applies the diff and stages all changes; patched files are
// staged even if they are ignored.
func applyAndStage(wt *git.Worktree, diffText string) error {
	written, err := patch.Apply(workdir, diffText)
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
	type key struct {
		num  string // number without leading zeros, "" = no number
		name string
	}
	var keys []key
	for _, e := range entries {
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".patch") {
			continue
		}
		digits := len(e.Name()) - len(strings.TrimLeft(e.Name(), "0123456789"))
		num := ""
		if digits > 0 {
			num = "#" + strings.TrimLeft(e.Name()[:digits], "0")
		}
		keys = append(keys, key{num, e.Name()})
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch {
		case (a.num == "") != (b.num == ""):
			return a.num != ""
		case len(a.num) != len(b.num):
			return len(a.num) < len(b.num)
		case a.num != b.num:
			return a.num < b.num
		}
		return a.name < b.name
	})
	files := make([]string, len(keys))
	for i, k := range keys {
		files[i] = filepath.Join(dir, k.name)
	}
	return files
}

func convertPatches(c *config) int {
	files := listPatches(c.PatchesDir)
	if len(files) == 0 {
		logGit.error("No patches to convert found")
		return exitOK
	}
	if err := os.MkdirAll("converted", 0o777); err != nil {
		logGit.fail(err, "Unable to create converted directory")
		return exitOSError
	}
	for _, file := range files {
		p := readPatch(file)
		name := filepath.Base(file)
		target := filepath.Join("converted", name)
		if p == nil {
			os.WriteFile(target, nil, 0o644) // Java creates the file before failing
			return crash("invalid patch " + name)
		}
		if err := os.WriteFile(target, []byte(p.Diff), 0o644); err != nil {
			logGit.fail(err, "Unable to write converted patches to disk")
			return exitOSError
		}
		logGit.info("Successfully converted patch %s", name)
	}
	return exitOK
}
