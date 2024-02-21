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

func ParseEnv() (err error) {
	return ParseEnvSet(flag.CommandLine, os.Environ())
}
func ParseEnvSet(fl *flag.FlagSet, env []string) (err error) {
	defer func() {
		err = mimicFlagSetError(fl, err)
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

func ParseConfig(path string) (err error) {
	return ParseConfigSet(flag.CommandLine, path)
}
func ParseConfigSet(fl *flag.FlagSet, path string) (err error) {
	if path == "" {
		return nil
	}
	defer func() {
		err = mimicFlagSetError(fl, err)
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

func mimicFlagSetError(fl *flag.FlagSet, err error) error {
	if err == nil {
		return nil
	}

	fmt.Fprintln(fl.Output(), err)
	if fl.Usage != nil {
		fl.Usage()
	}
	switch fl.ErrorHandling() {
	case flag.PanicOnError:
		panic(err)
	case flag.ExitOnError:
		os.Exit(2)
	}
	return err
}
