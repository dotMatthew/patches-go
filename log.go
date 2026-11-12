package main

import (
	"fmt"
	"os"
	"time"
)

// Exit codes.
const (
	exitOK       = 0
	exitOSError  = 1 // also used for errors the Java version did not handle
	exitUsage    = 8
	exitInternal = 42
)

// logger writes log lines in the format of the Java version (logback):
// "HH:mm:ss.SSS [main] LEVEL logger - message". The logger names are the
// abbreviated Java class names.
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

func (l logger) info(format string, args ...any)  { l.write("INFO", nil, format, args...) }
func (l logger) error(format string, args ...any) { l.write("ERROR", nil, format, args...) }

// fail logs an error message followed by the cause.
func (l logger) fail(err error, format string, args ...any) { l.write("ERROR", err, format, args...) }

func (l logger) write(level string, err error, format string, args ...any) {
	line := fmt.Sprintf("%s [main] %-5s %s - %s\n", time.Now().Format("15:04:05.000"), level, l, fmt.Sprintf(format, args...))
	if err != nil {
		line += err.Error() + "\n"
	}
	fmt.Fprint(os.Stdout, line)
}

// crash reports an error that the Java version did not handle (it ended up
// as an exception in picocli, which exits with code 1).
func crash(msg string) int {
	fmt.Fprintln(os.Stderr, "Exception: "+msg)
	return exitOSError
}
