/*
flagconf provides extensions to Go's flag package to support prefixed environment variables and a simple config file format.

# install

	$ go install go.senan.xyz/flagconf@latest

# example program

	func main() {
	    // stdlib flag
	    confPath := flag.String("config-path", "", "")
	    someString = flag.String("some-string", "", "some fancy string")
	    customArray = flag.String("some-bool", "", "some custom bool")

	    var sr stringArray // implements flag.Value
	    flag.Var(&sr, "string-array", "custom string array type with flag.Value")

	    flag.Parse()
	    flagconf.ParseEnv()
	    flagconf.ParseConfig(*confPath)
	}

# usage

	$ my-app -some-string str # use as normal

	# use env vars instead
	# prefix comes from FlagSet.Name() (defaults to os.Args[0], can be changed with flag.CommandLine.Init()
	$ env MY_APP_SOME_STRING=str my-app
	$ env MY_APP_SOME_STRING=str my-app -some-string other # cli takes priority

	$ my-app -string-array one -string-array two   # stack args for cli lists
	$ env MY_APP_SRING_ARRAY=one,two my-app        # use comma for env lists
	$ env MY_APP_SRING_ARRAY=one,two\,three my-app # escape delimiter with \ if you need

	$ cat conf
	# repeat keys for config file lists
	string-array one
	string-array two
	$ my-app -config-path conf

	$ env MY_APP_CONFIG_PATH=conf my-app # provide config path as env var if you like

	$ env MY_APP_SOME_STRING=a my-app -some-bool 1 -config-path conf # stack all 3
*/
package flagconf

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseEnv calls [ParseEnvSet] with the global [flag.CommandLine] and [os.Environ]. Note that err can safely be ignored
// if the [flag.ErrorHandling] is not [flag.ContinueOnError].
func ParseEnv() (err error) {
	return ParseEnvSet(flag.CommandLine, os.Environ())
}

// ParseEnvSet visits flags from fl that have not been provided yet, and finds corresponding values
// in the environment provided by env.
//
// If [flag.FlagSet.Name] is set, such as with [flag.FlagSet.Init], then that will be used as a prefix for
// the variable. For example a name of "my-app" would result in a variable like MY_APP_EXAMPLE_FLAG (Note that
// for the global [flag.CommandLine], the name defaults to os.Args[0] so Init() may not need to be called if
// that name suits your project already)
//
// Environment variables will be split on a "," making it possible to parse arrays. The array flag should implement
// [flag.Value] to that Set() can be called multiple times. The delimiter can be escaped with a backslash. For
// example MY_APP_ARRAY=a,b\,c,d will call [flag.Value.Set] for "a", "b,c", and "d".
//
// Flag values will also be expanded with [os.Expand] and the given env.
//
// Note that err can safely be ignored if the [flag.ErrorHandling] is not [flag.ContinueOnError].
func ParseEnvSet(fl *flag.FlagSet, env []string) (err error) {
	defer func() {
		mimicFlagSetError(fl, err)
	}()

	prefix := ReadEnvPrefix(fl)

	envMap := genEnvMap(env)
	expand := func(v string) string {
		return os.Expand(v, func(k string) string { return envMap[k] })
	}

	setFlags := getSetFlags(fl)

	var flagErrs []error
	fl.VisitAll(func(f *flag.Flag) {
		if _, ok := setFlags[f.Name]; ok {
			return
		}
		key := envKeyForFlag(prefix, f.Name)
		for _, v := range splitEscape(envMap[key], ",", `\`) {
			v = expand(v)
			if err := fl.Set(f.Name, v); err != nil {
				flagErrs = append(flagErrs, err)
				continue
			}
		}
	})

	return errors.Join(flagErrs...)
}

// ParseEnv calls [ParseConfigSet] with the global [flag.CommandLine] and [os.Environ]. Note that err can safely be ignored
// if the [flag.ErrorHandling] is not [flag.ContinueOnError].
func ParseConfig(path string) (err error) {
	return ParseConfigSet(flag.CommandLine, os.Environ(), path)
}

// ParseConfigSet visits flags from fl that have not been provided yet, and finds corresponding values in the
// config file specified by path.
//
// The format for the config file is one line per flag value, and key value
// pairs are delimited by a space character. Key values can be repeated making it possible to parse arrays.
// The array flag should implement [flag.Value] to that Set() can be called multiple times.
//
//	my-flag my value
//	my-array value one
//	my-array value two
//
// Comments are supported with the "#" character.
//
//	# ignore me
//	my-flag my value
//
// Flag values will also be expanded with [os.Expand] and the given env.
//
//	my-flag $HOME/dir
//
// It is not an error if the path argument, or the file that it points to, is empty.
func ParseConfigSet(fl *flag.FlagSet, env []string, path string) (err error) {
	if path == "" {
		return nil
	}
	defer func() {
		mimicFlagSetError(fl, err)
	}()

	envMap := genEnvMap(env)
	expand := func(v string) string {
		return os.Expand(v, func(k string) string { return envMap[k] })
	}

	path = expand(path)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()

	config := map[string][]string{}

	sc := bufio.NewScanner(file)
	for sc.Scan() {
		if k, v, ok := parseConfigLine(sc.Text()); ok {
			config[k] = append(config[k], v)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan config: %w", err)
	}

	setFlags := getSetFlags(fl)

	var flagErrs []error
	fl.VisitAll(func(f *flag.Flag) {
		if _, ok := setFlags[f.Name]; ok {
			return
		}
		for _, v := range config[f.Name] {
			v = expand(v)
			if err := fl.Set(f.Name, v); err != nil {
				flagErrs = append(flagErrs, fmt.Errorf("set %s: %w", f.Name, err))
				continue
			}
		}
	})

	return errors.Join(flagErrs...)
}

// UpdateConfig sets the values for key in the config file at path, in the format that [ParseConfigSet] reads. Every
// other line is left exactly as it is, including comments, blank lines, and keys that are not flags.
//
// Lines that already set key are rewritten in order, so that comments stay next to them. Values beyond those lines are
// added after the last one, or at the end of the file if there were none, and lines beyond the values are removed.
// With no values, key is removed from the file.
//
//	# calendars
//	my-array value one
//	# contacts
//	my-array value two
//
// After UpdateConfig(path, "my-array", "value 1", "value two", "value three") that file is
//
//	# calendars
//	my-array value 1
//	# contacts
//	my-array value two
//	my-array value three
//
// Values are written as given, so "$HOME/dir" stays unexpanded in the file and is expanded when it is read. A value
// that would not read back the same is an error: an empty one, or one with a line break or surrounding space.
//
// Like [ParseConfig], path is expanded with the environment. The file and its directory are created if they are
// missing. An existing file keeps its permissions and is replaced in one step, and a symlink is followed so that the
// file it points to is the one written. A file that already has the values is not written at all.
func UpdateConfig(path string, key string, values ...string) error {
	if key == "" || key != strings.TrimSpace(key) || strings.HasPrefix(key, "#") || strings.ContainsAny(key, "\t \r\n") {
		return fmt.Errorf("invalid config key %q", key)
	}
	for _, v := range values {
		if v == "" || v != strings.TrimSpace(v) || strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("invalid config value %q for %s", v, key)
		}
	}

	path = os.ExpandEnv(path)
	if path == "" {
		return errors.New("no config file path")
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read config file: %w", err)
	}

	eol := "\n"
	if strings.Contains(string(data), "\r\n") {
		eol = "\r\n"
	}

	var lines []string
	var rest = values
	var end = -1
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := parseConfigLine(line)
		if !ok || k != key {
			lines = append(lines, line)
			continue
		}
		if len(rest) == 0 {
			continue
		}
		if v != rest[0] {
			line = key + " " + rest[0] + eol
		}
		lines = append(lines, line)
		rest = rest[1:]
		end = len(lines)
	}

	if len(rest) > 0 {
		if end < 0 {
			end = len(lines)
		}
		if end > 0 && !strings.HasSuffix(lines[end-1], "\n") {
			lines[end-1] += eol
		}
		added := make([]string, 0, len(rest))
		for _, v := range rest {
			added = append(added, key+" "+v+eol)
		}
		lines = append(lines[:end], append(added, lines[end:]...)...)
	}

	updated := strings.Join(lines, "")
	if updated == string(data) {
		return nil
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	_, err = tmp.WriteString(updated)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temp config file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}

var ReadEnvPrefix = func(fl *flag.FlagSet) string {
	return filepath.Base(fl.Name())
}

func getSetFlags(fl *flag.FlagSet) map[string]struct{} {
	m := map[string]struct{}{}
	fl.Visit(func(f *flag.Flag) {
		m[f.Name] = struct{}{}
	})
	return m
}

func genEnvMap(env []string) map[string]string {
	envMap := map[string]string{}
	for _, en := range env {
		if k, v, ok := strings.Cut(en, "="); ok {
			envMap[k] = v
		}
	}
	return envMap
}

func parseConfigLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if idx := strings.IndexAny(line, "\t "); idx >= 0 {
		return line[:idx], strings.TrimSpace(line[idx:]), true
	}
	return line, "true", true
}

func envKeyForFlag(prefix string, name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ToUpper(name)
	if prefix == "" {
		return name
	}
	prefix = strings.ReplaceAll(prefix, "-", "_")
	prefix = strings.ToUpper(prefix)
	return prefix + "_" + name
}

func splitEscape(str string, sep, esc string) []string {
	if str == "" {
		return nil
	}
	str = strings.ReplaceAll(str, esc+sep, "\x00")
	tokens := strings.Split(str, sep)
	for i, token := range tokens {
		tokens[i] = strings.ReplaceAll(token, "\x00", sep)
	}
	return tokens
}

func mimicFlagSetError(fl *flag.FlagSet, err error) {
	if err == nil {
		return
	}

	fmt.Fprintln(fl.Output(), err)
	if fl.Usage != nil {
		fl.Usage()
	}
	switch fl.ErrorHandling() {
	case flag.ExitOnError:
		os.Exit(2)
	case flag.PanicOnError:
		panic(err)
	}
}
