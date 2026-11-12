package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

var (
	optConfig = option{"-F", "--config", "<configFile>"}
	optURL    = option{"-U", "--repo-url", "<url>"}
	optBRU    = option{"-BRU", "--base-repo-url", "<baseRepoUrl>"}
	optBRF    = option{"-BRF", "--base-repo-ref", "<baseRepoRef>"}
	optPDP    = option{"-PDP", "--patches-directory-path", "<patchesDirectoryPath>"}
)

const (
	configHelp = "  -F, --config=<configFile>\n         Path to patches config file. Defaults to patches.properties\n"
	configWide = "  -F, --config=<configFile>\n                         Path to patches config file. Defaults to patches.\n                           properties\n"
	urlHelp    = "  -U, --repo-url=<url>   URL of the Base Repo\n"
)

var commands []*command

func init() {
	commands = []*command{
		{"init", "Usage: patches init [-F=<configFile>] [-U=<url>]\n" +
			"Create a new patches file if not existing and downloads the base repository as\n" +
			"specified in patches.properties, but does not apply any patches.\n" + configWide + urlHelp,
			[]option{optConfig, optURL}, runInit},
		{"apply", "Usage: patches apply [-F=<configFile>]\nApplies the patches to the base repository\n" + configHelp,
			[]option{optConfig}, runApply},
		{"clean", "Usage: patches clean [-F=<configFile>]\nDelete all work files\n" + configHelp,
			[]option{optConfig}, runClean},
		{"create-patch", "Usage: patches create-patch [-F=<configFile>] [-U=<url>]\nCreates a new patch from the current commit\n" + configWide + urlHelp,
			[]option{optConfig, optURL}, runCreatePatch},
		{"config", "Usage: patches config [-BRF=<baseRepoRef>] [-BRU=<baseRepoUrl>]\n" +
			"                      [-F=<configFile>] [-PDP=<patchesDirectoryPath>]\n" +
			"Changes a value in the current patches config file\n" +
			"      -BRF, --base-repo-ref=<baseRepoRef>\n         Set the default repository ref\n" +
			"      -BRU, --base-repo-url=<baseRepoUrl>\n         Set the base repository url\n" + configHelp +
			"      -PDP, --patches-directory-path=<patchesDirectoryPath>\n         Set the path to the patches directory\n",
			[]option{optConfig, optBRU, optBRF, optPDP}, runConfig},
		{"reset", "Usage: patches reset [-F=<configFile>]\nResets the workdir repository to the config base ref and the remote head state\n" + configHelp,
			[]option{optConfig}, runReset},
		{"convert-patches", "Usage: patches convert-patches [-F=<configFile>]\nConvert our patch format to git patches\n" + configHelp,
			[]option{optConfig}, runConvert},
	}
}

func runInit(v values) int {
	file := v.get("--config", defaultConfigFile)
	var c *config
	created := false
	if exists(file) {
		c = loadConfig(file)
	} else {
		// The example is always written to patches.properties, even with -F.
		c = &config{URL: v.get("--repo-url", exampleURL), Ref: "main", PatchesDir: "patches"}
		if err := c.save(defaultConfigFile); err != nil {
			logProps.fail(err, "Failed to write example patches config file to disk")
			c = nil
		}
		created = c != nil
	}
	switch {
	case c == nil:
		logInit.error("No valid patches configuration found?")
		return exitUsage
	case c.URL == exampleURL && created:
		logInit.info("Create new base patches config file! Happy working :)")
		return exitOK
	case c.URL == exampleURL:
		logInit.error("Example url %s cannot be used to download the base repository! Aborting!", exampleURL)
		return exitUsage
	}
	if err := os.MkdirAll(c.PatchesDir, 0o777); err != nil {
		if fi, statErr := os.Stat(c.PatchesDir); statErr == nil && !fi.IsDir() {
			logInit.fail(err, "Patches directory exist but is not a directory!")
			return exitUsage
		}
		logInit.fail(err, "An error occurred at creating the patches directory")
	}
	return cloneRepo(c)
}

func runApply(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logApply.error("No patches config file found! Run patches init first!")
		return exitUsage
	}
	c := loadConfig(file)
	if c == nil {
		logApply.error("An error occurred at applying patches")
		return exitInternal
	}
	if fi, err := os.Stat(c.PatchesDir); err != nil || !fi.IsDir() {
		logApply.error("Patches directory path is not a directory!")
		return exitUsage
	}
	if !exists(workdir) {
		logApply.error("No workdir found!")
		return exitUsage
	}
	return applyPatches(c)
}

func runClean(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logClean.error("No patches config file found! Will not clean work directory")
		return exitInternal
	}
	if loadConfig(file) == nil {
		return crash("invalid patches configuration")
	}
	logClean.info("Deleting %s directory", workdir)
	if fi, err := os.Stat(workdir); err != nil || !fi.IsDir() {
		logFile.error("Git repo dir specified in config is not a directory or does not exist?")
		return exitOSError
	}
	// os.RemoveAll does not follow symlinks, so nothing outside is deleted.
	if err := os.RemoveAll(workdir); err != nil {
		logFile.fail(err, "Unable to delete workdir directory: %s", workdir)
	}
	return exitOK
}

func runCreatePatch(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logCreate.error("No valid patches configuration found!")
		return exitUsage
	}
	c := loadConfig(file)
	logCreate.info("Specify the name of the new patch file: (Without .patch extension)")
	name, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		logCreate.fail(err, "There was an error at reading from the default System input")
		return exitOSError
	}
	if name, _, _ = strings.Cut(name, "\n"); err != nil && name == "" {
		name = "null" // like Java's readLine() at end of input
	}
	name, _, _ = strings.Cut(name, "\r")
	if c == nil {
		return crash("invalid patches configuration")
	}
	return createPatch(c, name)
}

func runConfig(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logConfig.error("No patches config file found. Please run patches init first!")
		return exitInternal
	}
	c := loadConfig(file)
	if c == nil {
		// Java fails when the first change is applied, or after this log line
		_, url := v["--base-repo-url"]
		_, ref := v["--base-repo-ref"]
		_, dir := v["--patches-directory-path"]
		if !url && !ref && !dir {
			logConfig.info("Saving configuration changes to disk")
		}
		return crash("invalid patches configuration")
	}
	if s, ok := v["--base-repo-url"]; ok {
		logConfig.info("Changing base repo url to '%s' from '%s'", s, c.URL)
		c.URL = s
	}
	if s, ok := v["--base-repo-ref"]; ok {
		logConfig.info("Changing base repo ref to '%s' from '%s'", s, c.Ref)
		c.Ref = s
	}
	if s, ok := v["--patches-directory-path"]; ok {
		logConfig.info("Changing patches directory path to '%s' from '%s'", s, c.PatchesDir)
		if !exists(c.PatchesDir) || exists(s) {
			logFile.error("Either the current patches directory does not exist or the new name of the directory is already in use! Aborting!")
		} else {
			os.Rename(c.PatchesDir, s) // the config is changed even if this fails
		}
		c.PatchesDir = s
	}
	logConfig.info("Saving configuration changes to disk")
	if err := c.save(file); err != nil {
		logProps.fail(err, "Failed to save patches configuration file to disk")
		return exitOSError
	}
	return exitOK
}

func runReset(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logReset.error("Unable to reset repository. No config file found")
		return exitUsage
	}
	c := loadConfig(file)
	logReset.info("Starting to reset the workdir repository")
	if c == nil {
		return crash("invalid patches configuration")
	}
	return resetRepo(c)
}

func runConvert(v values) int {
	file := v.get("--config", defaultConfigFile)
	if !exists(file) {
		logConvert.error("No valid patches configuration file found!")
		return exitUsage
	}
	c := loadConfig(file)
	logConvert.info("Start to convert our patches to git patches")
	if c == nil {
		return crash("invalid patches configuration")
	}
	return convertPatches(c)
}
