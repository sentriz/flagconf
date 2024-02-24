package flagconf_test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.senan.xyz/flagconf"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		//nolint:errcheck
		"test": func() int {
			_ = flag.String("str", "", "")
			conf := flag.String("config-path", "", "")
			var fa flagArray
			flag.Var(&fa, "arr", "")
			var we willErr
			flag.Var(&we, "will-err", "")

			flag.Parse()
			flagconf.ParseEnv()
			flagconf.ParseConfig(*conf)

			flag.VisitAll(func(f *flag.Flag) {
				fmt.Printf("%s %v\n", f.Name, f.Value)
			})
			return 0
		},
	}))
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata",
		RequireExplicitExec: true,
	})
}

type flagArray []string

func (fa flagArray) String() string {
	var quoted []string
	for _, f := range fa {
		quoted = append(quoted, fmt.Sprintf("%q", f))
	}
	return strings.Join(quoted, ", ")
}

func (fa *flagArray) Set(value string) error {
	*fa = append(*fa, value)
	return nil
}

type willErr struct{}

func (willErr) String() string         { return "" }
func (willErr) Set(value string) error { return fmt.Errorf("invalid option for will-err") }
