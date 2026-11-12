package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	defaultConfigFile = "patches.toml"
	exampleURL        = "no://op"
	defaultPatchesDir = "patches"
)

// config is the content of patches.toml:
//
//	base_repo_url = "https://github.com/example/repo.git"
//	base_repo_ref = "main"
//	patches_directory = "patches"
type config struct {
	URL        string `toml:"base_repo_url"`
	Ref        string `toml:"base_repo_ref"`
	PatchesDir string `toml:"patches_directory,omitempty"`
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return path != "" && err == nil
}

// loadConfig reads a config file: TOML, or the old properties format if the
// file name ends with ".properties". The error describes why it is invalid
// (callers report it and decide how to go on).
func loadConfig(file string) (*config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, failure(exitCrash, logProps, err, "Failed to load patches configuration")
	}
	if isPropertiesFile(file) {
		return readPropertiesConfig(data)
	}
	var c config
	meta, err := toml.Decode(string(data), &c)
	if err == nil {
		if unknown := meta.Undecoded(); len(unknown) > 0 {
			err = fmt.Errorf("unknown keys: %v", unknown)
		}
	}
	if err != nil {
		return nil, failure(exitCrash, logProps, err, "Failed to load patches configuration")
	}
	if !meta.IsDefined("base_repo_url") || !meta.IsDefined("base_repo_ref") {
		return nil, &cmdError{code: exitCrash, logger: logProps, level: "INFO", msg: "Keys cannot be loaded from config file!"}
	}
	if c.PatchesDir == "" {
		c.PatchesDir = defaultPatchesDir
	}
	return &c, nil
}

// save writes the config as TOML.
func (c *config) save(file string) error {
	var buf bytes.Buffer
	buf.WriteString("# patches configuration\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return os.WriteFile(file, buf.Bytes(), 0o644)
}

func isPropertiesFile(file string) bool { return strings.HasSuffix(file, ".properties") }

// tomlFile returns the TOML file that belongs to a config file name
// ("x.properties" -> "x.toml").
func tomlFile(file string) string {
	if isPropertiesFile(file) {
		return strings.TrimSuffix(file, ".properties") + ".toml"
	}
	return file
}

// resolveConfigFile returns the config file to use for the -F value file.
// An old ".properties" config (given with -F or found next to a missing
// ".toml" config) is migrated to TOML. If it cannot be migrated because it is
// invalid, its path is returned so the caller reports why, as before.
func (a *app) resolveConfigFile(file string) string {
	target := tomlFile(file)
	if exists(target) {
		return target
	}
	old := file
	if !isPropertiesFile(old) {
		old = strings.TrimSuffix(file, ".toml") + ".properties"
	}
	if !isPropertiesFile(old) || !exists(old) {
		return target
	}
	if err := a.migrateConfig(old, target); err != nil {
		return old
	}
	return target
}

// migrateConfig converts the properties file old to the TOML file target
// and renames old to "<old>.bak".
func (a *app) migrateConfig(old, target string) error {
	c, err := loadConfig(old)
	if err != nil {
		return err // reported when the caller loads the old file
	}
	if err := c.save(target); err != nil {
		a.logErr(logProps, err, "Unable to migrate %s to %s", old, target)
		return err
	}
	if err := os.Rename(old, old+".bak"); err != nil {
		a.logErr(logProps, err, "Unable to rename %s to %s.bak", old, old)
	}
	a.info(logProps, "Migrated %s to %s (the old file was renamed to %s.bak)", a.rel(old), a.rel(target), a.rel(old))
	return nil
}

// rel returns p relative to the project directory (for log messages).
func (a *app) rel(p string) string {
	if r, err := filepath.Rel(a.dir, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}
