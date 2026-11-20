package flagconf_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestUpdateConfig(t *testing.T) {
	tests := []struct {
		name   string
		before string
		values []string
		after  string
	}{
		{"new file", "", []string{"$HOME/a b", "c"}, "dir $HOME/a b\ndir c\n"},
		{"in place", "# top\n\n# one\n dir\tone \n# two\ndir two\n\naddr x\n", []string{"one", "2", "3"}, "# top\n\n# one\n dir\tone \n# two\ndir 2\ndir 3\n\naddr x\n"},
		{"fewer", "# one\ndir one\n# two\ndir two\naddr x\n", []string{"1"}, "# one\ndir 1\n# two\naddr x\n"},
		{"remove", "dir one\n#dir no\ndirs y\ndir two\n", nil, "#dir no\ndirs y\n"},
		{"crlf", "addr x\r\ndir one\r\n", []string{"1", "2"}, "addr x\r\ndir 1\r\ndir 2\r\n"},
		{"no final newline", "addr x", []string{"a"}, "addr x\ndir a\n"},
		{"unchanged", "addr x\ndir a", []string{"a"}, "addr x\ndir a"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sub", "conf")
			if tc.before != "" {
				_ = os.Mkdir(filepath.Dir(path), 0o755)
				_ = os.WriteFile(path, []byte(tc.before), 0o644)
			}

			if err := flagconf.UpdateConfig(path, "dir", tc.values...); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(path); string(got) != tc.after {
				t.Errorf("\ngot  %q\nwant %q", got, tc.after)
			}
		})
	}
}

func TestUpdateConfigSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs symlinks and unix permissions")
	}

	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	_ = os.WriteFile(target, []byte("dir old\n"), 0o640)
	_ = os.Symlink("target", link)

	if err := flagconf.UpdateConfig(link, "dir", "new"); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced: %v", err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("the permissions changed: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "dir new\n" {
		t.Errorf("got %q", got)
	}
}

func TestUpdateConfigInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf")
	for _, kv := range [][2]string{{"", "a"}, {"my key", "a"}, {"#key", "a"}, {"key", ""}, {"key", "a\nb"}, {"key", " a"}} {
		if err := flagconf.UpdateConfig(path, kv[0], kv[1]); err == nil {
			t.Errorf("no error for %q", kv)
		}
	}
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
