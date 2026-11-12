package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

func cloneRepo(c *config) int {
	logGit.info("Start to download the repository")
	if fi, err := os.Stat(workdir); err == nil {
		if entries, _ := os.ReadDir(workdir); !fi.IsDir() || len(entries) > 0 {
			return crash(fmt.Sprintf("Destination path %q already exists and is not an empty directory", workdir))
		}
	}
	if err := clone(c.URL, c.Ref); err != nil {
		logGit.fail(err, "An error occurred at downloading the repository")
		return exitInternal
	}
	logGit.info("Successfully downloaded the repository")
	return exitOK
}

// clone clones url into the work directory and checks out ref, which may be
// a full ref name, a branch or a tag (tags are checked out detached).
func clone(url, ref string) error {
	remote := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		return fmt.Errorf("Invalid remote: origin: %w", err)
	}
	opts := &git.CloneOptions{URL: url, Tags: git.AllTags}
	if ref != "HEAD" {
		for _, name := range []string{ref, "refs/heads/" + ref, "refs/tags/" + ref} {
			for _, r := range refs {
				if opts.ReferenceName == "" && r.Name().String() == name {
					opts.ReferenceName = r.Name()
				}
			}
		}
		if opts.ReferenceName == "" {
			return fmt.Errorf("Remote branch '%s' not found in upstream origin", ref)
		}
	}
	existed := exists(workdir)
	if _, err = git.PlainClone(workdir, false, opts); err != nil && !existed {
		os.RemoveAll(workdir)
	}
	return err
}

func openWorktree() (*git.Repository, *git.Worktree, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return nil, nil, err
	}
	wt, err := repo.Worktree()
	return repo, wt, err
}

func isClean(wt *git.Worktree) (bool, error) {
	st, err := wt.Status()
	return err == nil && st.IsClean(), err
}

func resetRepo(c *config) int {
	if !exists(workdir) {
		logGit.error("No git directory found to reset!")
		return exitUsage
	}
	code, err := reset(c.Ref)
	if err != nil {
		logGit.fail(err, "An error occurred at resetting repository")
		return exitInternal
	}
	return code
}

func reset(ref string) (int, error) {
	repo, wt, err := openWorktree()
	if err != nil {
		return 0, err
	}
	if err := cleanWorktree(repo, wt); err != nil {
		return 0, err
	}
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"},
		Tags:       git.AllTags,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return 0, err
	}
	target, err := resolveCommit(repo, ref)
	if err != nil {
		return 0, err
	}
	if target == nil {
		logGit.error("Could not found commit id for base ref")
		return exitInternal, nil
	}
	if err := hardReset(repo, wt, *target); err != nil {
		return 0, err
	}
	logGit.info("Successfully reset the repository")
	return exitOK, nil
}

// resolveCommit resolves a full sha, a ref name, a remote branch, a tag or a
// local branch (in this order) to a commit; nil if nothing matches.
func resolveCommit(repo *git.Repository, ref string) (*plumbing.Hash, error) {
	if len(ref) == 40 && strings.Trim(strings.ToLower(ref), "0123456789abcdef") == "" {
		return peel(repo, plumbing.NewHash(strings.ToLower(ref)))
	}
	var candidates []string
	if strings.HasPrefix(ref, "refs/") {
		candidates = append(candidates, ref)
	}
	candidates = append(candidates, "refs/remotes/origin/"+ref, "refs/tags/"+ref, "refs/heads/"+ref,
		ref, "refs/"+ref, "refs/remotes/"+ref)
	for _, name := range candidates {
		if r, err := repo.Reference(plumbing.ReferenceName(name), true); err == nil {
			return peel(repo, r.Hash())
		}
	}
	return nil, nil
}

// peel follows annotated tags to the commit.
func peel(repo *git.Repository, h plumbing.Hash) (*plumbing.Hash, error) {
	for {
		obj, err := repo.Object(plumbing.AnyObject, h)
		if err != nil {
			return nil, err
		}
		switch o := obj.(type) {
		case *object.Commit:
			return &h, nil
		case *object.Tag:
			h = o.Target
		default:
			return nil, fmt.Errorf("object %s is not a commit", h)
		}
	}
}

// cleanWorktree deletes untracked files and every non-ignored directory
// without tracked files (with all its content). Ignored files are kept.
func cleanWorktree(repo *git.Repository, wt *git.Worktree) error {
	idx, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	tracked := map[string]bool{"": true}
	for _, e := range idx.Entries {
		for p := e.Name; p != "."; p = path.Dir(p) {
			tracked[p] = true
		}
	}
	patterns, _ := gitignore.ReadPatterns(wt.Filesystem, nil)
	root := osfs.New("/")
	if global, err := gitignore.LoadGlobalPatterns(root); err == nil {
		patterns = append(global, patterns...)
	}
	if system, err := gitignore.LoadSystemPatterns(root); err == nil {
		patterns = append(system, patterns...)
	}
	ignored := gitignore.NewMatcher(patterns)

	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(filepath.Join(workdir, dir))
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := path.Join(dir, e.Name())
			if p == ".git" || ignored.Match(strings.Split(p, "/"), e.IsDir()) || (tracked[p] && !e.IsDir()) {
				continue
			}
			abs := filepath.Join(workdir, filepath.FromSlash(p))
			if e.IsDir() && tracked[p] {
				err = walk(p)
			} else {
				err = os.RemoveAll(abs)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return walk("")
}

// hardReset moves HEAD (or the current branch) to target, resets the index
// and checks out all tracked files. Unlike go-git's HardReset it keeps
// untracked and ignored files.
func hardReset(repo *git.Repository, wt *git.Worktree, target plumbing.Hash) error {
	idx, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	trackedBefore := map[string]bool{}
	for _, e := range idx.Entries {
		trackedBefore[e.Name] = true
	}
	if err := wt.Reset(&git.ResetOptions{Commit: target, Mode: git.MixedReset}); err != nil {
		return err
	}
	commit, err := repo.CommitObject(target)
	if err != nil {
		return err
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	status, err := wt.Status()
	if err != nil {
		return err
	}
	for p, s := range status {
		abs := filepath.Join(workdir, filepath.FromSlash(p))
		switch {
		case s.Worktree == git.Untracked && trackedBefore[p]: // not part of target
			os.Remove(abs)
			for dir := filepath.Dir(abs); dir != workdir && os.Remove(dir) == nil; dir = filepath.Dir(dir) {
			}
		case s.Worktree == git.Modified || s.Worktree == git.Deleted:
			if err := checkout(tree, p, abs); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkout(tree *object.Tree, p, abs string) error {
	entry, err := tree.FindEntry(p)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
		return err
	}
	if entry.Mode == filemode.Submodule {
		return os.Mkdir(abs, 0o777)
	}
	file, err := tree.TreeEntryFile(entry)
	if err != nil {
		return err
	}
	content, err := file.Contents()
	if err != nil {
		return err
	}
	switch entry.Mode {
	case filemode.Symlink:
		return os.Symlink(content, abs)
	case filemode.Executable:
		return os.WriteFile(abs, []byte(content), 0o777)
	}
	return os.WriteFile(abs, []byte(content), 0o666)
}
