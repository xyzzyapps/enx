package pipeline_test

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/lang"
	"enx/internal/pipeline"
)

// anaphoraCase is one typed-anaphora program and the referents it must produce.
type anaphoraCase struct {
	lang string
	text string
	want []string
}

var typedAnaphora = []anaphoraCase{
	{
		lang: "c",
		text: "int main(void) {\n  double ratio = 2.5;\n  int count = 10;\n  int again = !-1:int;\n  double twice = !-1:double * 2.0;\n  return 0;\n}\n",
		want: []string{"int again = count;", "double twice = ratio * 2.0;"},
	},
	{
		lang: "c",
		text: "int main(void) {\n  double ratio = 2.5;\n  int count = 10;\n  int total = !-1:double + !-1:int;\n  return 0;\n}\n",
		want: []string{"int total = ratio + count;"},
	},
	{
		lang: "ruby",
		text: "class Point\nend\np = Point.new\nn = 5\ns = \"text\"\na = !-1:String\nb = !-1:Point\nc = !-1:Integer\n",
		want: []string{"a = s", "b = p", "c = n"},
	},
	{
		lang: "perl",
		text: "package Point;\nsub new { 1 }\npackage main;\nmy $p = Point->new;\nmy @list = (1, 2);\nmy %map = (a => 1);\nmy $a = !-1:Point;\nmy $b = !-1:array;\nmy $c = !-1:hash;\n",
		want: []string{"my $a = $p;", "my $b = @list;", "my $c = %map;"},
	},
}

// TestTypedAnaphora is the feature: the filter narrows the history, then the
// ordinal indexes into it.
func TestTypedAnaphora(t *testing.T) {
	for _, tc := range typedAnaphora {
		t.Run(tc.lang+"/"+short(tc.want[0]), func(t *testing.T) {
			opts := options(t, tc.lang)
			opts.AnaphorMarker = '!'
			res, err := pipeline.Run(core.NewSource("a.enx", []byte(tc.text)), lang.New(), opts)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := res.Output(opts)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("output does not contain %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestTypedAnaphoraFiltersBeforeIndexing pins the rule: !-2:Point is the second
// most recent Point, not the second binding overall (which might be an Integer).
func TestTypedAnaphoraFiltersBeforeIndexing(t *testing.T) {
	text := "class Point\nend\na = Point.new\nn = 5\nb = Point.new\nc = !-2:Point\n"
	opts := options(t, "ruby")
	opts.AnaphorMarker = '!'
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if !strings.Contains(got, "c = a") {
		t.Fatalf("!-2:Point should be the first Point, got:\n%s", got)
	}
}

// TestKeepProseWithAnaphora pins that the two output options compose: the
// dropped English becomes comments and the markers still become names.
func TestKeepProseWithAnaphora(t *testing.T) {
	text := "we set the stock\nstock = 10\nmugs = stock - 3\ntotal = !-1 + !-2\n"
	opts := options(t, "ruby")
	opts.AnaphorMarker = '!'
	opts.KeepProse = true
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if !strings.Contains(got, "# we set the stock") {
		t.Fatalf("dropped English was not kept as a comment:\n%s", got)
	}
	if strings.Contains(got, "!-1") || strings.Contains(got, "!-2") {
		t.Fatalf("a marker survived with --keep-prose:\n%s", got)
	}
	if !strings.Contains(got, "mugs + stock") {
		t.Fatalf("the marker was not substituted:\n%s", got)
	}
}

// TestUnwrittenTypeIsNotGuessed is the design boundary: a method call result has
// no written type, so a typed anaphor must not reach it.
func TestUnwrittenTypeIsNotGuessed(t *testing.T) {
	text := "p = compute()\nq = !-1:String\n"
	opts := options(t, "ruby")
	opts.AnaphorMarker = '!'
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.Output(opts); !strings.Contains(got, "!-1:String") {
		t.Fatalf("a binding with no written type was used as a referent:\n%s", got)
	}
	if res.Anaphora == nil || len(res.Anaphora.Unresolved) != 1 {
		t.Fatal("the unresolved marker was not reported")
	}
}

// TestMultipleAssignmentDoesNotGuessTypes covers the list form: "a, b = 1,
// \"x\"" gives b a string, and reading the first right hand side for every
// target would silently type b as an Integer.
func TestMultipleAssignmentDoesNotGuessTypes(t *testing.T) {
	text := "a, b = 1, \"x\"\ny = !-1:Integer\n"
	opts := options(t, "ruby")
	opts.AnaphorMarker = '!'
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.Output(opts); !strings.Contains(got, "!-1:Integer") {
		t.Fatalf("a multiple-assignment target was typed from the wrong value:\n%s", got)
	}
	if res.Anaphora == nil || len(res.Anaphora.Unresolved) != 1 {
		t.Fatal("the typed marker should be unresolved")
	}
}

// TestClassReturningConstructorsAreExcluded covers the deny list: Struct.new
// returns a class, not a Struct.
func TestClassReturningConstructorsAreExcluded(t *testing.T) {
	text := "X = Struct.new(:a)\ny = !-1:Struct\n"
	opts := options(t, "ruby")
	opts.AnaphorMarker = '!'
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.Output(opts); !strings.Contains(got, "!-1:Struct") {
		t.Fatalf("Struct.new was treated as a Struct instance:\n%s", got)
	}
}

// TestPointerQualifiedType pins the pointer form, which is expressible precisely
// because "*" is no longer the marker.
func TestPointerQualifiedType(t *testing.T) {
	text := "int main(void) {\n  char *label = \"x\";\n  char letter = 'a';\n  char *ptr = !-1:char*;\n  char plain = !-1:char;\n  return 0;\n}\n"
	opts := options(t, "c")
	opts.AnaphorMarker = '!'
	res, err := pipeline.Run(core.NewSource("a.enx", []byte(text)), lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if !strings.Contains(got, "char *ptr = label;") {
		t.Fatalf("!-1:char* should be the pointer binding, got:\n%s", got)
	}
	// Without the star, either kind matches, and the most recent is the pointer
	// just assigned.
	if !strings.Contains(got, "char plain = ptr;") {
		t.Fatalf("!-1:char should match any char binding, got:\n%s", got)
	}
}

func short(s string) string {
	if len(s) > 24 {
		return s[:24]
	}
	return s
}
