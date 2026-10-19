package pipeline_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"enx/internal/lang"
	"enx/internal/loader"
	"enx/internal/pipeline"
)

// exampleSets maps the book-inspired example directories to host languages.
// The expected stdout lives beside each .enx file.
var exampleSets = []struct {
	dir  string
	lang string
}{
	{dir: "eloquent_ruby", lang: "ruby"},
	{dir: "perl_cookbook", lang: "perl"},
	{dir: "c_programming_language", lang: "c"},
	{dir: "showcase/ruby", lang: "ruby"},
	{dir: "showcase/perl", lang: "perl"},
	{dir: "showcase/c", lang: "c"},
	{dir: "strict", lang: "ruby"},
}

func languageConfig(name string) language {
	for _, lg := range languages {
		if lg.name == name {
			return lg
		}
	}
	return language{name: name}
}

// TestExamplesEmitAndRun keeps the examples honest: each one must decode to a
// non-empty program and, when its toolchain is available, produce the recorded
// stdout.
func TestExamplesEmitAndRun(t *testing.T) {
	for _, set := range exampleSets {
		lg := languageConfig(set.lang)
		root := filepath.Join("..", "..", "examples", set.dir)
		inputs, err := filepath.Glob(filepath.Join(root, "*.enx"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		if len(inputs) == 0 {
			t.Fatalf("no examples in %s", root)
		}

		for _, in := range inputs {
			name := filepath.Base(in)
			t.Run(set.lang+"/"+name, func(t *testing.T) {
				want, err := os.ReadFile(in + ".stdout")
				if err != nil {
					t.Fatalf("read expected stdout: %v", err)
				}
				loaded, err := loader.Load(in)
				if err != nil {
					t.Fatalf("load example: %v", err)
				}

				opts := options(t, set.lang)
				opts.AnaphorMarker = '!'
				res, err := pipeline.Run(loaded, lang.New(), opts)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				emitted := res.Output(opts)
				if strings.TrimSpace(emitted) == "" {
					t.Fatal("the example emitted an empty program")
				}
				wantEmitted, err := os.ReadFile(in + ".expected")
				if err != nil {
					t.Fatalf("read expected emission: %v", err)
				}
				if emitted != string(wantEmitted) {
					t.Fatalf("emission differs:\n--- got ---\n%s\n--- want ---\n%s", emitted, wantEmitted)
				}

				runner := lg.compiler
				if runner == "" {
					runner = lg.interpreter
				}
				if _, err := exec.LookPath(runner); err != nil {
					t.Logf("%s is not on PATH, checking emission only", runner)
					return
				}

				dir := t.TempDir()
				srcPath := filepath.Join(dir, name+res.Spec.Extension)
				if err := os.WriteFile(srcPath, []byte(emitted), 0o600); err != nil {
					t.Fatalf("write emitted program: %v", err)
				}

				var command []string
				if lg.compiler != "" {
					exe := filepath.Join(dir, "prog.exe")
					args := append([]string{}, lg.compileArgs...)
					args = append(args, "-o", exe, srcPath)
					if b, err := exec.Command(runner, args...).CombinedOutput(); err != nil {
						t.Fatalf("%s rejected the example: %v\n%s", runner, err, b)
					}
					command = []string{exe}
				} else {
					if b, err := exec.Command(runner, lg.syntaxFlag, srcPath).CombinedOutput(); err != nil {
						t.Fatalf("%s rejected the example: %v\n%s", runner, err, b)
					}
					command = []string{runner, srcPath}
				}

				got, err := exec.Command(command[0], command[1:]...).Output()
				if err != nil {
					t.Fatalf("running the example failed: %v", err)
				}
				if normalise(string(got)) != normalise(string(want)) {
					t.Fatalf("stdout = %q, want %q", got, want)
				}
			})
		}
	}
}
