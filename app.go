package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// Exit codes.
const (
	exitOK       = 0
	exitCrash    = 1 // errors the Java version did not handle (and some I/O errors)
	exitUsage    = 8
	exitInternal = 42
)

// app holds everything a command needs from its environment, so commands
// can run against any directory and any input/output.
type app struct {
	ctx    context.Context
	dir    string // project directory; relative paths are resolved against it
	stdin  io.Reader
	stdout io.Writer // log output
	stderr io.Writer // unhandled errors
}

// path resolves p against the project directory.
func (a *app) path(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(a.dir, p)
}

// workdir is the checkout of the base repository.
func (a *app) workdir() string { return a.path("_workdir") }

// logger is the (abbreviated) Java class name shown in log lines.
type logger string

const (
	logCLI     logger = "dev.taiqane.patches.cli.PatchesCLI"
	logInit    logger = "d.t.patches.cli.command.InitCommand"
	logApply   logger = "d.t.patches.cli.command.ApplyCommand"
	logClean   logger = "d.t.patches.cli.command.CleanCommand"
	logCreate  logger = "d.t.p.cli.command.CreatePatchCommand"
	logConfig  logger = "d.t.p.cli.command.ConfigCommand"
	logReset   logger = "d.t.patches.cli.command.ResetCommand"
	logConvert logger = "d.t.p.c.c.ConvertPatchesCommand"
	logGit     logger = "d.t.patches.internal.git.GitService"
	logPatch   logger = "d.t.patches.internal.git.DiffService"
	logFile    logger = "d.t.p.internal.file.FileService"
	logProps   logger = "d.t.p.c.PatchesConfiguration"
)

func (a *app) info(l logger, format string, args ...any) { a.log("INFO", l, nil, format, args...) }

func (a *app) error(l logger, format string, args ...any) { a.log("ERROR", l, nil, format, args...) }

// logErr logs an error with its cause without ending the command.
func (a *app) logErr(l logger, cause error, format string, args ...any) {
	a.log("ERROR", l, cause, format, args...)
}

// log writes "HH:mm:ss.SSS [main] LEVEL logger - message" (the logback
// format of the Java version), followed by the cause if there is one.
func (a *app) log(level string, l logger, cause error, format string, args ...any) {
	line := fmt.Sprintf("%s [main] %-5s %s - %s\n", time.Now().Format("15:04:05.000"), level, l, fmt.Sprintf(format, args...))
	if cause != nil {
		line += cause.Error() + "\n"
	}
	_, _ = io.WriteString(a.stdout, line)
}

// cmdError ends a command with an exit code. If logger is set, it is reported
// as an ERROR log line; otherwise it is an error the Java version did not
// handle, which is printed to stderr.
type cmdError struct {
	code   int
	logger logger
	level  string // log level, "ERROR" if empty
	msg    string
	cause  error
}

func (e *cmdError) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *cmdError) Unwrap() error { return e.cause }

// failure returns an error that is logged by l and exits with code.
func failure(code int, l logger, cause error, format string, args ...any) error {
	return &cmdError{code: code, logger: l, msg: fmt.Sprintf(format, args...), cause: cause}
}

// crash returns an error the Java version did not handle (exit code 1).
func crash(format string, args ...any) error {
	return &cmdError{code: exitCrash, msg: fmt.Sprintf(format, args...)}
}

// report prints err and returns its exit code (0 for nil).
func (a *app) report(err error) int {
	if err == nil {
		return exitOK
	}
	var e *cmdError
	if !errors.As(err, &e) {
		e = &cmdError{code: exitCrash, msg: err.Error()}
	}
	if e.logger == "" {
		fmt.Fprintln(a.stderr, "Exception: "+e.Error())
	} else {
		level := e.level
		if level == "" {
			level = "ERROR"
		}
		a.log(level, e.logger, e.cause, "%s", e.msg)
	}
	return e.code
}
