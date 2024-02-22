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
// Note that err can safely be ignored if the [flag.ErrorHandling] is not [flag.ContinueOnError].
func ParseEnvSet(fl *flag.FlagSet, env []string) (err error) {
	defer func() {
		mimicFlagSetError(fl, err)
	}()

	envMap := map[string]string{}
	for _, en := range env {
		if k, v, ok := strings.Cut(en, "="); ok {
			envMap[k] = v
		}
	}

	setFlags := getSetFlags(fl)
	prefix := filepath.Base(fl.Name())

	var flagErrs []error
	fl.VisitAll(func(f *flag.Flag) {
		if _, ok := setFlags[f.Name]; ok {
			return
		}
		if vs := envMap[envKeyForFlag(prefix, f.Name)]; vs != "" {
			for _, v := range splitEscape(vs, ",", `\`) {
				flagErrs = append(flagErrs, f.Value.Set(v))
			}
		}
	})

	return errors.Join(flagErrs...)
}

// ParseEnv calls [ParseConfigSet] with the global [flag.CommandLine]. Note that err can safely be ignored
// if the [flag.ErrorHandling] is not [flag.ContinueOnError].
func ParseConfig(path string) (err error) {
	return ParseConfigSet(flag.CommandLine, path)
}

// ParseConfigSet visits flags from fl that have not been provided yet, and finds corresponding values in the
// config file specified by path. The format for the config file is one line per flag value, and key value
// pairs are delimited by a space character. Key values can be repeated making it possible to parse arrays.
// The array flag should implement [flag.Value] to that Set() can be called multiple times. For example
//
//	my-flag my value
//	my-array value one
//	my-array value two
func ParseConfigSet(fl *flag.FlagSet, path string) (err error) {
	if path == "" {
		return nil
	}
	defer func() {
		mimicFlagSetError(fl, err)
	}()

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()

	allFlags := getAllFlags(fl)
	config := map[string][]string{}

	sc := bufio.NewScanner(file)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		if strings.HasPrefix(k, "#") {
			continue
		}
		if _, ok := allFlags[k]; !ok {
			return fmt.Errorf("unknown config option %q", k)
		}
		if v := strings.TrimSpace(v); v != "" {
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
			flagErrs = append(flagErrs, f.Value.Set(v))
		}
	})

	return errors.Join(flagErrs...)
}

func getAllFlags(fl *flag.FlagSet) map[string]struct{} {
	m := map[string]struct{}{}
	fl.VisitAll(func(f *flag.Flag) {
		m[f.Name] = struct{}{}
	})
	return m
}
func getSetFlags(fl *flag.FlagSet) map[string]struct{} {
	m := map[string]struct{}{}
	fl.Visit(func(f *flag.Flag) {
		m[f.Name] = struct{}{}
	})
	return m
}

func envKeyForFlag(prefix string, name string) string {
	prefix = strings.ReplaceAll(prefix, "-", "_")
	name = strings.ReplaceAll(name, "-", "_")
	k := strings.Join([]string{prefix, name}, "_")
	k = strings.ToUpper(k)
	return k
}

func splitEscape(string string, sep, esc string) []string {
	string = strings.ReplaceAll(string, esc+sep, "\x00")
	tokens := strings.Split(string, sep)
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
