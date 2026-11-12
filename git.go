package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// cloneRepo clones the base repository into the work directory.
func (a *app) cloneRepo(c *config) error {
	a.info(logGit, "Start to download the repository")
	if fi, err := os.Stat(a.workdir()); err == nil {
		if entries, _ := os.ReadDir(a.workdir()); !fi.IsDir() || len(entries) > 0 {
			return crash("destination path %q already exists and is not an empty directory", "_workdir")
		}
	}
	if err := a.clone(c.URL, c.Ref); err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at downloading the repository")
	}
	a.info(logGit, "Successfully downloaded the repository")
	return nil
}

// clone clones url and checks out ref, which may be a full ref name, a
// branch or a tag (tags are checked out detached).
func (a *app) clone(url, ref string) error {
	remote := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := remote.ListContext(a.ctx, &git.ListOptions{})
	if err != nil {
		return fmt.Errorf("invalid remote origin: %w", err)
	}
	opts := &git.CloneOptions{URL: url, Tags: git.AllTags}
	if ref != "HEAD" {
		for _, name := range []string{ref, "refs/heads/" + ref, "refs/tags/" + ref} {
			if slices.ContainsFunc(refs, func(r *plumbing.Reference) bool { return r.Name().String() == name }) {
				opts.ReferenceName = plumbing.ReferenceName(name)
				break
			}
		}
		if opts.ReferenceName == "" {
			return fmt.Errorf("remote branch %q not found in upstream origin", ref)
		}
	}
	existed := exists(a.workdir())
	_, err = git.PlainCloneContext(a.ctx, a.workdir(), false, opts)
	if err != nil && !existed {
		_ = os.RemoveAll(a.workdir()) // best effort cleanup of a partial clone
	}
	return err
}

func (a *app) openWorktree() (*git.Repository, *git.Worktree, error) {
	repo, err := git.PlainOpen(a.workdir())
	if err != nil {
		return nil, nil, err
	}
	wt, err := repo.Worktree()
	return repo, wt, err
}

func isClean(wt *git.Worktree) (bool, error) {
	st, err := wt.Status()
	if err != nil {
		return false, err
	}
	return st.IsClean(), nil
}

// resetRepo cleans the work directory, fetches and resets it to the base ref.
func (a *app) resetRepo(c *config) error {
	if !exists(a.workdir()) {
		return failure(exitUsage, logGit, nil, "No git directory found to reset!")
	}
	repo, wt, err := a.openWorktree()
	if err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at resetting repository")
	}
	target, err := a.fetchAndResolve(repo, wt, c.Ref)
	if err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at resetting repository")
	}
	if target == nil {
		return failure(exitInternal, logGit, nil, "Could not found commit id for base ref")
	}
	if err := a.hardReset(repo, wt, *target); err != nil {
		return failure(exitInternal, logGit, err, "An error occurred at resetting repository")
	}
	a.info(logGit, "Successfully reset the repository")
	return nil
}

// fetchAndResolve cleans the work directory, fetches all branches and tags
// and resolves ref (nil if it does not exist).
func (a *app) fetchAndResolve(repo *git.Repository, wt *git.Worktree, ref string) (*plumbing.Hash, error) {
	if err := a.cleanWorktree(repo, wt); err != nil {
		return nil, err
	}
	err := repo.FetchContext(a.ctx, &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"},
		Tags:       git.AllTags,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, err
	}
	return resolveCommit(repo, ref)
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
func (a *app) cleanWorktree(repo *git.Repository, wt *git.Worktree) error {
	idx, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	tracked := map[string]bool{"": true} // tracked files and their parent directories
	for _, e := range idx.Entries {
		for p := e.Name; p != "."; p = path.Dir(p) {
			tracked[p] = true
		}
	}
	ignored, err := ignoreMatcher(wt)
	if err != nil {
		return err
	}

	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(filepath.Join(a.workdir(), dir))
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := path.Join(dir, e.Name())
			if p == ".git" || ignored.Match(strings.Split(p, "/"), e.IsDir()) || (tracked[p] && !e.IsDir()) {
				continue
			}
			if e.IsDir() && tracked[p] {
				err = walk(p)
			} else {
				err = os.RemoveAll(filepath.Join(a.workdir(), filepath.FromSlash(p)))
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return walk("")
}

// ignoreMatcher combines the system, global and repository ignore rules.
func ignoreMatcher(wt *git.Worktree) (gitignore.Matcher, error) {
	patterns, err := gitignore.ReadPatterns(wt.Filesystem, nil)
	if err != nil {
		return nil, err
	}
	root := osfs.New("/")
	// A missing or unreadable global/system config simply means no rules.
	if global, err := gitignore.LoadGlobalPatterns(root); err == nil {
		patterns = append(global, patterns...)
	}
	if system, err := gitignore.LoadSystemPatterns(root); err == nil {
		patterns = append(system, patterns...)
	}
	return gitignore.NewMatcher(patterns), nil
}

// hardReset moves HEAD (or the current branch) to target, resets the index
// and checks out all tracked files. Unlike go-git's HardReset it keeps
// untracked and ignored files.
func (a *app) hardReset(repo *git.Repository, wt *git.Worktree, target plumbing.Hash) error {
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
		abs := filepath.Join(a.workdir(), filepath.FromSlash(p))
		switch {
		case s.Worktree == git.Untracked && trackedBefore[p]: // not part of target
			if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			a.removeEmptyParents(filepath.Dir(abs))
		case s.Worktree == git.Modified || s.Worktree == git.Deleted:
			if err := checkout(tree, p, abs); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeEmptyParents removes dir and its parents up to the work directory as
// long as they are empty.
func (a *app) removeEmptyParents(dir string) {
	for ; dir != a.workdir() && strings.HasPrefix(dir, a.workdir()); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil { // not empty (or already gone): stop
			return
		}
	}
}

// checkout writes the file p of tree to abs.
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
	default:
		return os.WriteFile(abs, []byte(content), 0o666)
	}
}
