package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// The command line behaves like the picocli based Java version: same usage
// texts, error messages, suggestions and exit codes.

type option struct {
	short, long string // e.g. "-F", "--config"
	param       string // e.g. "<configFile>"
}

// values holds the parsed options by long name.
type values map[string]string

func (v values) get(long, def string) string {
	if s, ok := v[long]; ok {
		return s
	}
	return def
}

type command struct {
	name    string
	usage   string
	options []option
	run     func(values) int
}

func (c *command) find(name string) *option {
	for i, o := range c.options {
		if name == o.short || name == o.long {
			return &c.options[i]
		}
	}
	return nil
}

func (c *command) optionNames() []string {
	if c == nil {
		return []string{"-h", "--help", "-V", "--version"}
	}
	var names []string
	for _, o := range c.options {
		names = append(names, o.short, o.long)
	}
	return names
}

const rootUsage = `Usage: patches [-hV] [COMMAND]
A command line tool for managing Git patches
  -h, --help      Show this help message and exit.
  -V, --version   Print version information and exit.
Commands:
  init             Create a new patches file if not existing and downloads the
                     base repository as specified in patches.properties, but
                     does not apply any patches.
  apply            Applies the patches to the base repository
  clean            Delete all work files
  create-patch     Creates a new patch from the current commit
  config           Changes a value in the current patches config file
  reset            Resets the workdir repository to the config base ref and the
                     remote head state
  convert-patches  Convert our patch format to git patches
`

// usageError is a picocli parameter error: exit code 2, message on stderr.
type usageError struct{ msg, usage, hint string }

func (e *usageError) print(w io.Writer) int {
	if e.hint != "" {
		fmt.Fprintf(w, "%s\n%s\n", e.msg, e.hint)
	} else {
		fmt.Fprintf(w, "%s\n%s", e.msg, e.usage)
	}
	return 2
}

func execute(args []string, stdout, stderr io.Writer) int {
	help, version := false, false
	var cmd *command
	var opts values
	var unmatched []string
	unmatchedAt := 0

	i := 0
loop:
	for ; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help":
			help = true
		case a == "-V" || a == "--version":
			version = true
		case len(a) > 2 && a[0] == '-' && a[1] != '-' && strings.Trim(a[1:], "hV") == "":
			help = help || strings.Contains(a, "h")
			version = version || strings.Contains(a, "V")
		case a == "--":
			i++
			break loop
		default:
			for _, c := range commands {
				if c.name == a {
					cmd = c
					break loop
				}
			}
			break loop
		}
	}
	if cmd != nil {
		var err *usageError
		opts, unmatched, unmatchedAt, err = cmd.parse(args, i+1)
		if err != nil {
			return err.print(stderr)
		}
	} else if i < len(args) {
		unmatched, unmatchedAt = args[i:], i
	}
	switch {
	case unmatched != nil && !help && !version:
		return unmatchedError(cmd, unmatched, unmatchedAt).print(stderr)
	case help:
		fmt.Fprint(stdout, rootUsage)
		return 0
	case version:
		fmt.Fprintln(stdout, "patches "+currentVersion())
		return 0
	case cmd == nil:
		logCLI.error("No command specified. Use 'patches --help' for usage information.")
		return exitOK
	}
	return cmd.run(opts)
}

// isOption reports whether arg would be taken as an option (picocli does not
// accept it as an option parameter then).
func (c *command) isOption(arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	return c.find(name) != nil || (len(arg) > 2 && arg[0] == '-' && c.find(arg[:2]) != nil)
}

// parse parses the options of a subcommand starting at args[start]. It
// returns the remaining (unmatched) arguments and their index.
func (c *command) parse(args []string, start int) (values, []string, int, *usageError) {
	v := values{}
	fail := func(format string, a ...any) (values, []string, int, *usageError) {
		return nil, nil, 0, &usageError{msg: fmt.Sprintf(format, a...), usage: c.usage}
	}
	for i := start; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return v, args[i+1:], i + 1, nil
			}
			break
		}
		var o *option
		var value string
		if o = c.find(a); o != nil {
			if i+1 == len(args) {
				return fail("Missing required parameter for option '%s' (%s)", o.long, o.param)
			}
			if i++; c.isOption(args[i]) {
				return fail("Expected parameter for option '%s' but found '%s'", o.long, args[i])
			}
			value = args[i]
		} else if name, val, ok := strings.Cut(a, "="); ok && c.find(name) != nil {
			o, value = c.find(name), val
		} else if len(a) > 2 && a[0] == '-' && a[1] != '-' && c.find(a[:2]) != nil {
			o, value = c.find(a[:2]), a[2:]
		} else {
			return v, args[i:], i, nil
		}
		if _, dup := v[o.long]; dup {
			return fail("option '%s' (%s) should be specified only once", o.long, o.param)
		}
		v[o.long] = value
	}
	return v, nil, 0, nil
}

func unmatchedError(cmd *command, unmatched []string, at int) *usageError {
	e := &usageError{usage: rootUsage}
	if cmd != nil {
		e.usage = cmd.usage
	}
	quoted := "'" + strings.Join(unmatched, "', '") + "'"
	names := cmd.optionNames()
	arg := unmatched[0]
	plural := len(unmatched) > 1
	if resemblesOption(arg, names) {
		e.msg = map[bool]string{false: "Unknown option: ", true: "Unknown options: "}[plural] + quoted
		prefix := strings.TrimLeft(arg, "-")
		prefix = prefix[:min(2, len(prefix))]
		var hints []string
		for _, n := range names {
			if strings.HasPrefix(strings.TrimLeft(n, "-"), prefix) {
				hints = append(hints, n)
			}
		}
		if len(hints) > 0 {
			e.hint = "Possible solutions: " + strings.Join(hints, ", ")
		}
		return e
	}
	if plural {
		e.msg = fmt.Sprintf("Unmatched arguments from index %d: %s", at, quoted)
	} else {
		e.msg = fmt.Sprintf("Unmatched argument at index %d: %s", at, quoted)
	}
	if cmd == nil {
		if similar := similarCommands(arg); len(similar) > 0 {
			e.hint = "Did you mean: patches " + strings.Join(similar, " or patches ") + "?"
		}
	}
	return e
}

// resemblesOption is picocli's heuristic to tell "unknown option" from
// "unmatched argument".
func resemblesOption(arg string, names []string) bool {
	if len(arg) <= 1 {
		return false
	}
	count := 0
	for _, name := range names {
		for i := 0; i < len(arg) && i < len(name) && arg[i] == name[i]; i++ {
			count++
		}
	}
	return count > 0 && count*10 >= len(names)*9
}

// similarCommands returns up to 3 commands ordered by the cosine similarity
// of their character bigrams with arg (picocli's suggestion algorithm).
func similarCommands(arg string) []string {
	bigrams := func(s string) map[string]float64 {
		m := map[string]float64{}
		for i := 0; i+2 <= len(s); i++ {
			m[s[i:i+2]]++
		}
		return m
	}
	dot := func(a, b map[string]float64) (r float64) {
		for k, v := range a {
			r += v * b[k]
		}
		return r
	}
	a := bigrams(strings.ToLower(arg))
	byScore := map[float64]string{} // equal scores: the later command wins
	for _, c := range commands {
		b := bigrams(c.name)
		if score := dot(a, b) / math.Sqrt(dot(a, a)*dot(b, b)); score > 0 {
			byScore[score] = c.name
		}
	}
	var scores []float64
	for s := range byScore {
		scores = append(scores, s)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	var names []string
	for _, s := range scores[:min(3, len(scores))] {
		names = append(names, byScore[s])
	}
	return names
}
