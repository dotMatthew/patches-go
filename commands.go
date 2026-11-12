package main

import (
	"bufio"
	"embed"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/dotMatthew/patches-go/internal/cli"
)

//go:embed usage
var usageFiles embed.FS

func usage(name string) string {
	b, err := usageFiles.ReadFile("usage/" + name + ".txt")
	if err != nil {
		panic(err) // the files are embedded at compile time
	}
	return string(b)
}

const (
	optConfig = "--config"
	optURL    = "--repo-url"
	optBRU    = "--base-repo-url"
	optBRF    = "--base-repo-ref"
	optPDP    = "--patches-directory-path"
	optName   = "--name"
)

var (
	configOption = cli.Option{Short: "-F", Long: optConfig, Param: "<configFile>"}
	urlOption    = cli.Option{Short: "-U", Long: optURL, Param: "<url>"}
)

// run executes the command line and returns the exit code.
func (a *app) run(args []string) int {
	cmd := func(name string, run func(cli.Values) error, opts ...cli.Option) *cli.Command {
		return &cli.Command{Name: name, Usage: usage(name), Options: opts,
			Run: func(v cli.Values) int { return a.report(run(v)) }}
	}
	app := &cli.App{
		Name:    "patches",
		Usage:   usage("patches"),
		Version: currentVersion(),
		Stdout:  a.stdout,
		Stderr:  a.stderr,
		Root: func() int {
			a.error(logCLI, "No command specified. Use 'patches --help' for usage information.")
			return exitOK
		},
		Commands: []*cli.Command{
			cmd("init", a.runInit, configOption, urlOption),
			cmd("apply", a.runApply, configOption),
			cmd("clean", a.runClean, configOption),
			cmd("create-patch", a.runCreatePatch, configOption,
				cli.Option{Short: "-n", Long: optName, Param: "<name>"}),
			cmd("config", a.runConfig, configOption,
				cli.Option{Short: "-BRU", Long: optBRU, Param: "<baseRepoUrl>"},
				cli.Option{Short: "-BRF", Long: optBRF, Param: "<baseRepoRef>"},
				cli.Option{Short: "-PDP", Long: optPDP, Param: "<patchesDirectoryPath>"}),
			cmd("reset", a.runReset, configOption),
			cmd("convert-patches", a.runConvert, configOption),
		},
	}
	return app.Execute(args)
}

// configFile returns the config file selected with -F (default
// patches.toml); an old patches.properties is migrated to TOML first.
func (a *app) configFile(v cli.Values) string {
	return a.resolveConfigFile(a.path(v.Get(optConfig, defaultConfigFile)))
}

// requireConfig loads the config file selected with -F.
func (a *app) requireConfig(v cli.Values) (*config, error) {
	file := a.configFile(v)
	if !exists(file) {
		return nil, failure(exitUsage, logProps, nil, "No config file %s found. Run patches init first!", a.rel(file))
	}
	return loadConfig(file)
}

func (a *app) runInit(v cli.Values) error {
	file := a.configFile(v)
	created := false
	if !exists(file) {
		example := &config{URL: v.Get(optURL, exampleURL), Ref: "main", PatchesDir: defaultPatchesDir}
		if err := example.save(file); err != nil {
			return failure(exitIO, logProps, err, "Unable to write the config file %s", a.rel(file))
		}
		created = true
	}
	c, err := loadConfig(file)
	if err != nil {
		return err
	}
	if c.URL == exampleURL {
		if created {
			a.info(logInit, "Created the config file %s. Set base_repo_url (or use -U) and run patches init again.", a.rel(file))
			return nil
		}
		return failure(exitUsage, logInit, nil, "The example url %s in %s cannot be used to download the base repository. Set base_repo_url first!", exampleURL, a.rel(file))
	}
	dir := a.path(c.PatchesDir)
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		return failure(exitUsage, logInit, nil, "The patches directory %s exists but is not a directory!", c.PatchesDir)
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return failure(exitIO, logInit, err, "Unable to create the patches directory %s", c.PatchesDir)
	}
	return a.cloneRepo(c)
}

func (a *app) runApply(v cli.Values) error {
	c, err := a.requireConfig(v)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(a.path(c.PatchesDir)); err != nil || !fi.IsDir() {
		return failure(exitUsage, logApply, nil, "The patches directory %s does not exist!", c.PatchesDir)
	}
	if !exists(a.workdir()) {
		return failure(exitUsage, logApply, nil, "No work directory found. Run patches init first!")
	}
	return a.applyPatches(c)
}

func (a *app) runClean(v cli.Values) error {
	if _, err := a.requireConfig(v); err != nil {
		return err
	}
	if fi, err := os.Stat(a.workdir()); err != nil || !fi.IsDir() {
		return failure(exitUsage, logClean, nil, "No work directory found, nothing to clean")
	}
	a.info(logClean, "Deleting the work directory _workdir")
	// os.RemoveAll does not follow symlinks, so nothing outside is deleted.
	if err := os.RemoveAll(a.workdir()); err != nil {
		return failure(exitIO, logFile, err, "Unable to delete the work directory _workdir")
	}
	return nil
}

func (a *app) runCreatePatch(v cli.Values) error {
	c, err := a.requireConfig(v)
	if err != nil {
		return err
	}
	name, ok := v[optName]
	if !ok {
		a.info(logCreate, "Specify the name of the new patch file: (Without .patch extension)")
		if name, err = readLine(a.stdin); err != nil {
			return failure(exitIO, logCreate, err, "Unable to read the patch name")
		}
	}
	name = strings.TrimSuffix(strings.TrimSpace(name), ".patch")
	switch {
	case name == "":
		return failure(exitUsage, logCreate, nil, "No patch name given")
	case strings.ContainsAny(name, `/\`) || name == "." || name == "..":
		return failure(exitUsage, logCreate, nil, "Invalid patch name %q: it must not contain path separators", name)
	}
	return a.createPatch(c, name)
}

// readLine reads one line (without line terminator); "" at the end of input.
func readLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line, _, _ = strings.Cut(line, "\n")
	line, _, _ = strings.Cut(line, "\r")
	return line, nil
}

func (a *app) runConfig(v cli.Values) error {
	c, err := a.requireConfig(v)
	if err != nil {
		return err
	}
	if !v.Has(optBRU) && !v.Has(optBRF) && !v.Has(optPDP) {
		return failure(exitUsage, logConfig, nil, "Nothing to change: use --base-repo-url, --base-repo-ref or --patches-directory-path")
	}
	if s, ok := v[optBRU]; ok {
		a.info(logConfig, "Changing base repo url to '%s' from '%s'", s, c.URL)
		c.URL = s
	}
	if s, ok := v[optBRF]; ok {
		a.info(logConfig, "Changing base repo ref to '%s' from '%s'", s, c.Ref)
		c.Ref = s
	}
	if s, ok := v[optPDP]; ok {
		a.info(logConfig, "Changing patches directory path to '%s' from '%s'", s, c.PatchesDir)
		if err := a.movePatchesDir(c.PatchesDir, s); err != nil {
			return err
		}
		c.PatchesDir = s
	}
	file := tomlFile(a.configFile(v))
	a.info(logConfig, "Saving configuration changes to %s", a.rel(file))
	if err := c.save(file); err != nil {
		return failure(exitIO, logProps, err, "Unable to write the config file %s", a.rel(file))
	}
	return nil
}

// movePatchesDir renames the patches directory. If it does not exist yet,
// only the config changes; an existing target directory is never replaced.
func (a *app) movePatchesDir(from, to string) error {
	src, dst := a.path(from), a.path(to)
	switch {
	case !exists(src):
		return nil
	case exists(dst):
		return failure(exitUsage, logFile, nil, "The patches directory %s already exists. Move the patches manually!", to)
	}
	if err := os.Rename(src, dst); err != nil {
		return failure(exitIO, logFile, err, "Unable to rename the patches directory %s to %s", from, to)
	}
	return nil
}

func (a *app) runReset(v cli.Values) error {
	c, err := a.requireConfig(v)
	if err != nil {
		return err
	}
	a.info(logReset, "Starting to reset the workdir repository")
	return a.resetRepo(c)
}

func (a *app) runConvert(v cli.Values) error {
	c, err := a.requireConfig(v)
	if err != nil {
		return err
	}
	a.info(logConvert, "Start to convert our patches to git patches")
	return a.convertPatches(c)
}
