// Package cli is a small command line parser that behaves like picocli (the
// parser used by the original Java version): subcommands with options that
// take exactly one parameter, "-h"/"-V" on the root command, and picocli's
// error messages, suggestions and exit code 2 for usage errors.
package cli

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

// Option is an option that takes exactly one parameter.
type Option struct {
	Short string // e.g. "-F"
	Long  string // e.g. "--config"
	Param string // e.g. "<configFile>"
}

// Values are the parsed options of a command, keyed by the long name.
type Values map[string]string

// Get returns the value of an option or def if it was not given.
func (v Values) Get(long, def string) string {
	if s, ok := v[long]; ok {
		return s
	}
	return def
}

// Has reports whether the option was given.
func (v Values) Has(long string) bool {
	_, ok := v[long]
	return ok
}

// Command is a subcommand.
type Command struct {
	Name    string
	Usage   string // printed on usage errors
	Options []Option
	Run     func(Values) int // returns the exit code
}

// App is the root command.
type App struct {
	Name     string
	Usage    string
	Version  string
	Commands []*Command
	// Root runs when no subcommand is given.
	Root           func() int
	Stdout, Stderr io.Writer
}

// Execute parses the arguments, runs the selected command and returns the
// exit code (2 for usage errors).
func (app *App) Execute(args []string) int {
	help, version := false, false
	var cmd *Command
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
			idx := slices.IndexFunc(app.Commands, func(c *Command) bool { return c.Name == a })
			if idx >= 0 {
				cmd = app.Commands[idx]
			}
			break loop
		}
	}

	var values Values
	var unmatched []string
	unmatchedAt := i
	if cmd != nil {
		var err *usageError
		values, unmatched, unmatchedAt, err = cmd.parse(args, i+1)
		if err != nil {
			return err.print(app.Stderr)
		}
	} else if i < len(args) {
		unmatched = args[i:]
	}

	switch {
	case unmatched != nil && !help && !version:
		return app.unmatchedError(cmd, unmatched, unmatchedAt).print(app.Stderr)
	case help:
		fmt.Fprint(app.Stdout, app.Usage)
		return 0
	case version:
		fmt.Fprintln(app.Stdout, app.Name+" "+app.Version)
		return 0
	case cmd == nil:
		return app.Root()
	}
	return cmd.Run(values)
}

func (c *Command) find(name string) *Option {
	if i := slices.IndexFunc(c.Options, func(o Option) bool { return name == o.Short || name == o.Long }); i >= 0 {
		return &c.Options[i]
	}
	return nil
}

// isOption reports whether arg would be taken as an option (picocli does not
// accept it as an option parameter then).
func (c *Command) isOption(arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	return c.find(name) != nil || (len(arg) > 2 && arg[0] == '-' && c.find(arg[:2]) != nil)
}

// parse parses the options of a subcommand starting at args[start]. It
// returns the remaining (unmatched) arguments and the index of the first one.
func (c *Command) parse(args []string, start int) (Values, []string, int, *usageError) {
	v := Values{}
	fail := func(format string, a ...any) (Values, []string, int, *usageError) {
		return nil, nil, 0, &usageError{msg: fmt.Sprintf(format, a...), usage: c.Usage}
	}
	for i := start; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return v, args[i+1:], i + 1, nil
			}
			break
		}
		var o *Option
		var value string
		if o = c.find(a); o != nil { // "-F value", "--config value"
			if i+1 == len(args) {
				return fail("Missing required parameter for option '%s' (%s)", o.Long, o.Param)
			}
			if i++; c.isOption(args[i]) {
				return fail("Expected parameter for option '%s' but found '%s'", o.Long, args[i])
			}
			value = args[i]
		} else if name, val, ok := strings.Cut(a, "="); ok && c.find(name) != nil { // "--config=value"
			o, value = c.find(name), val
		} else if len(a) > 2 && a[0] == '-' && a[1] != '-' && c.find(a[:2]) != nil { // "-Fvalue"
			o, value = c.find(a[:2]), a[2:]
		} else {
			return v, args[i:], i, nil
		}
		if v.Has(o.Long) {
			return fail("option '%s' (%s) should be specified only once", o.Long, o.Param)
		}
		v[o.Long] = value
	}
	return v, nil, 0, nil
}

// usageError is reported with exit code 2: the message followed by either a
// hint or the usage of the command.
type usageError struct{ msg, usage, hint string }

func (e *usageError) print(w io.Writer) int {
	if e.hint != "" {
		fmt.Fprintf(w, "%s\n%s\n", e.msg, e.hint)
	} else {
		fmt.Fprintf(w, "%s\n%s", e.msg, e.usage)
	}
	return 2
}

func (app *App) unmatchedError(cmd *Command, unmatched []string, at int) *usageError {
	e := &usageError{usage: app.Usage}
	names := []string{"-h", "--help", "-V", "--version"}
	if cmd != nil {
		e.usage = cmd.Usage
		names = nil
		for _, o := range cmd.Options {
			names = append(names, o.Short, o.Long)
		}
	}
	quoted := "'" + strings.Join(unmatched, "', '") + "'"
	plural := len(unmatched) > 1
	arg := unmatched[0]

	if resemblesOption(arg, names) {
		if plural {
			e.msg = "Unknown options: " + quoted
		} else {
			e.msg = "Unknown option: " + quoted
		}
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
		if similar := app.similarCommands(arg); len(similar) > 0 {
			e.hint = "Did you mean: " + app.Name + " " + strings.Join(similar, " or "+app.Name+" ") + "?"
		}
	}
	return e
}

// resemblesOption is picocli's heuristic to tell an unknown option from an
// unmatched argument: the argument shares a prefix with most option names.
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

// similarCommands returns up to three commands ordered by the cosine
// similarity of their character bigrams with arg (picocli's algorithm; for
// equal scores the later command wins).
func (app *App) similarCommands(arg string) []string {
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
	byScore := map[float64]string{}
	for _, c := range app.Commands {
		b := bigrams(c.Name)
		if score := dot(a, b) / math.Sqrt(dot(a, a)*dot(b, b)); score > 0 { // NaN is not > 0
			byScore[score] = c.Name
		}
	}
	scores := make([]float64, 0, len(byScore))
	for s := range byScore {
		scores = append(scores, s)
	}
	slices.Sort(scores)
	slices.Reverse(scores)
	names := make([]string, 0, 3)
	for _, s := range scores[:min(3, len(scores))] {
		names = append(names, byScore[s])
	}
	return names
}
