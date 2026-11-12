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
			cmd("create-patch", a.runCreatePatch, configOption, urlOption),
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

// configFile returns the config file selected with -F.
func (a *app) configFile(v cli.Values) string { return a.path(v.Get(optConfig, defaultConfigFile)) }

func (a *app) runInit(v cli.Values) error {
	file := a.configFile(v)
	var c *config
	created := false
	if exists(file) {
		var err error
		if c, err = loadConfig(file); err != nil {
			a.report(err)
			c = nil
		}
	} else {
		// The example is always written to patches.properties, even with -F.
		c = &config{URL: v.Get(optURL, exampleURL), Ref: "main", PatchesDir: "patches"}
		if err := c.save(a.path(defaultConfigFile)); err != nil {
			a.logErr(logProps, err, "Failed to write example patches config file to disk")
			c = nil
		}
		created = c != nil
	}
	switch {
	case c == nil:
		return failure(exitUsage, logInit, nil, "No valid patches configuration found?")
	case c.URL == exampleURL && created:
		a.info(logInit, "Create new base patches config file! Happy working :)")
		return nil
	case c.URL == exampleURL:
		return failure(exitUsage, logInit, nil, "Example url %s cannot be used to download the base repository! Aborting!", exampleURL)
	}
	dir := a.path(c.PatchesDir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		if fi, statErr := os.Stat(dir); statErr == nil && !fi.IsDir() {
			return failure(exitUsage, logInit, err, "Patches directory exist but is not a directory!")
		}
		a.logErr(logInit, err, "An error occurred at creating the patches directory")
	}
	return a.cloneRepo(c)
}

func (a *app) runApply(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitUsage, logApply, nil, "No patches config file found! Run patches init first!")
	}
	c, err := loadConfig(file)
	if err != nil {
		a.report(err)
		return failure(exitInternal, logApply, nil, "An error occurred at applying patches")
	}
	if fi, err := os.Stat(a.path(c.PatchesDir)); err != nil || !fi.IsDir() {
		return failure(exitUsage, logApply, nil, "Patches directory path is not a directory!")
	}
	if !exists(a.workdir()) {
		return failure(exitUsage, logApply, nil, "No workdir found!")
	}
	return a.applyPatches(c)
}

func (a *app) runClean(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitInternal, logClean, nil, "No patches config file found! Will not clean work directory")
	}
	if _, err := loadConfig(file); err != nil {
		a.report(err)
		return crash("invalid patches configuration")
	}
	a.info(logClean, "Deleting _workdir directory")
	if fi, err := os.Stat(a.workdir()); err != nil || !fi.IsDir() {
		return failure(exitCrash, logFile, nil, "Git repo dir specified in config is not a directory or does not exist?")
	}
	// os.RemoveAll does not follow symlinks, so nothing outside is deleted.
	if err := os.RemoveAll(a.workdir()); err != nil {
		a.logErr(logFile, err, "Unable to delete workdir directory: _workdir")
	}
	return nil
}

func (a *app) runCreatePatch(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitUsage, logCreate, nil, "No valid patches configuration found!")
	}
	c, configErr := loadConfig(file)
	a.report(configErr)
	a.info(logCreate, "Specify the name of the new patch file: (Without .patch extension)")
	name, err := readLine(a.stdin)
	if err != nil {
		return failure(exitCrash, logCreate, err, "There was an error at reading from the default System input")
	}
	if configErr != nil {
		return crash("invalid patches configuration")
	}
	return a.createPatch(c, name)
}

// readLine reads one line like Java's BufferedReader.readLine(), which
// returns null at the end of the input (the Java version then uses "null").
func readLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if line == "" && err != nil {
		return "null", nil
	}
	line, _, _ = strings.Cut(line, "\n")
	line, _, _ = strings.Cut(line, "\r")
	return line, nil
}

func (a *app) runConfig(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitInternal, logConfig, nil, "No patches config file found. Please run patches init first!")
	}
	c, err := loadConfig(file)
	if err != nil {
		a.report(err)
		// Java fails at the first change, or after logging that it saves.
		if !v.Has(optBRU) && !v.Has(optBRF) && !v.Has(optPDP) {
			a.info(logConfig, "Saving configuration changes to disk")
		}
		return crash("invalid patches configuration")
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
		from, to := a.path(c.PatchesDir), a.path(s)
		if !exists(from) || exists(to) {
			a.error(logFile, "Either the current patches directory does not exist or the new name of the directory is already in use! Aborting!")
		} else {
			// Like in the Java version a failed rename is ignored and the
			// config is changed anyway.
			_ = os.Rename(from, to)
		}
		c.PatchesDir = s
	}
	a.info(logConfig, "Saving configuration changes to disk")
	if err := c.save(file); err != nil {
		return failure(exitCrash, logProps, err, "Failed to save patches configuration file to disk")
	}
	return nil
}

func (a *app) runReset(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitUsage, logReset, nil, "Unable to reset repository. No config file found")
	}
	c, err := loadConfig(file)
	a.report(err)
	a.info(logReset, "Starting to reset the workdir repository")
	if err != nil {
		return crash("invalid patches configuration")
	}
	return a.resetRepo(c)
}

func (a *app) runConvert(v cli.Values) error {
	file := a.configFile(v)
	if !exists(file) {
		return failure(exitUsage, logConvert, nil, "No valid patches configuration file found!")
	}
	c, err := loadConfig(file)
	a.report(err)
	a.info(logConvert, "Start to convert our patches to git patches")
	if err != nil {
		return crash("invalid patches configuration")
	}
	return a.convertPatches(c)
}
