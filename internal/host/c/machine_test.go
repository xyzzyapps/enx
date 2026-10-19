package c

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

// keepEverything is a cost model in which no token may be English, so the
// grammar must accept the input exactly as written.
type keepEverything struct{ toks []host.Token }

func (k keepEverything) Keep(int) float64 { return 0 }

func (k keepEverything) Skip(i int) float64 {
	if k.toks[i].IsTrivia() {
		return 0
	}
	return host.ImpossibleCost
}

func decodeKeepingAll(t *testing.T, text string) (*host.Path, []host.Token) {
	t.Helper()
	src := core.NewSource("test.c", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lex diagnostics: %v", diags)
	}
	path, ok := NewMachine().Decode(toks, keepEverything{toks: toks})
	if !ok {
		t.Fatalf("grammar rejected:\n%s", text)
	}
	return path, toks
}

// TestGrammarAcceptsRealC is the coverage test.
func TestGrammarAcceptsRealC(t *testing.T) {
	snippets := []string{
		"int x = 5;\n",
		"int x;\n",
		"char c = 'a';\n",
		"unsigned long n = 1UL;\n",
		"const char *name = \"text\";\n",
		"static int add(int a, int b) { return a + b; }\n",
		"int main(void) { return 0; }\n",
		"void f(void) { }\n",
		"double area(double r) {\n    return 3.14159 * r * r;\n}\n",
		"#include <stdio.h>\n",
		"#include \"local.h\"\n",
		"#define MAX 10\n",
		"#define LONG(a) \\\n  ((a) + 1)\nint x;\n",
		"#ifdef DEBUG\nint y;\n#endif\n",
		"struct point { double x, y; };\n",
		"struct point p = { .x = 1, .y = 2 };\n",
		"union u { int i; float f; };\n",
		"enum color { RED, GREEN, BLUE };\n",
		"enum color { RED = 1, GREEN = 2 };\n",
		"typedef struct { int a; } thing_t;\n",
		"typedef unsigned long size_type;\n",
		"int arr[3] = { 1, 2, 3 };\n",
		"int arr[] = { 1, 2, 3 };\n",
		"int m[2][3];\n",
		"int *p = &x;\n",
		"int **pp = &p;\n",
		"int (*fp)(int, int);\n",
		"void f(int a, char *b, double c) { }\n",
		"void f() { }\n",
		"int old_style(a, b) int a; int b; { return a; }\n",
		"int k_and_r(a, b) { return a; }\n",
		"if (x > 5) { y = 1; }\n",
		"if (x > 5) { y = 1; } else { y = 2; }\n",
		"if (a) if (b) x = 1; else x = 2;\n",
		"while (i < 10) { i++; }\n",
		"while (1);\n",
		"do { x--; } while (x > 0);\n",
		"for (int i = 0; i < n; i++) { sum += i; }\n",
		"for (;;) break;\n",
		"for (i = 0; i < n; ++i) ;\n",
		"switch (n) { case 1: a = 1; break; default: a = 0; }\n",
		"switch (c) { case 'a': case 'b': x = 1; break; }\n",
		"goto done;\n",
		"done: return 0;\n",
		"return;\n",
		"return (a + b) * c;\n",
		"break;\n",
		"continue;\n",
		";\n",
		"{ int inner = 1; }\n",
		"p->x = arr[i++];\n",
		"a = b ? c : d;\n",
		"x = (int)y;\n",
		"n = sizeof(int);\n",
		"n = sizeof x;\n",
		"f(a, b);\n",
		"printf(\"%d\\n\", n);\n",
		"printf(\"a\" \"b\");\n",
		"i = j = 0;\n",
		"x <<= 2;\n",
		"x |= y & z;\n",
		"a = b == c && d != e;\n",
		"p = malloc(n * sizeof(int));\n",
		"free(p);\n",
		"strcpy(dst, src);\n",
		"s->len = strlen(s->data);\n",
		"for (int i = 0; i < n; i++) {\n    printf(\"%d\\n\", a[i]);\n}\n",
		"static const double PI = 3.14159;\n",
		"extern int g;\n",
		"register int i;\n",
		"volatile int v;\n",
		"long long big = 1LL << 40;\n",
		"if (!x) return -1;\n",
		"x = -y;\n",
		"x = *p;\n",
		"x = a / b % c;\n",
		"while (getchar() != EOF) { n++; }\n",
		"typedef int (*handler)(void *ctx, int code);\n",
		"struct node { struct node *next; int value; };\n",
		"n = (a > b) ? a : b;\n",
		"int x = _Generic(v, int: 1, default: 0);\n",
		"struct S { unsigned x : 3; };\n",
		"__attribute__((packed)) struct S { int x; };\n",
		"__declspec(dllexport) int f(void);\n",
		"_Static_assert(sizeof(int) == 4, \"int\");\n",
		"_Alignas(16) int x;\n",
		"int x = (int){ 1 };\n",
	}

	for _, text := range snippets {
		t.Run(strings.SplitN(text, "\n", 2)[0], func(t *testing.T) {
			path, toks := decodeKeepingAll(t, text)
			for i, tok := range toks {
				if tok.IsTrivia() {
					continue
				}
				if path.Actions[i] != host.KeepToken {
					t.Fatalf("dropped %v from:\n%s", tok, text)
				}
			}
		})
	}
}

// TestEnglishWordsAreNotDeclarations checks the structural shelter C has: two
// adjacent identifiers are never valid, so an English run is not a declaration.
func TestEnglishWordsAreNotDeclarations(t *testing.T) {
	src := core.NewSource("t.c", []byte("the total value of the order\nint x = 5;\n"))
	toks, _ := New().Lex(src)
	costs := &allowSkip{toks: toks}
	path, ok := NewMachine().Decode(toks, costs)
	if !ok {
		t.Fatal("decode failed")
	}
	var kept []string
	for i, tok := range toks {
		if tok.IsTrivia() {
			continue
		}
		if path.Actions[i] == host.KeepToken {
			kept = append(kept, tok.Text)
		}
	}
	got := strings.Join(kept, " ")
	for _, word := range []string{"total", "value", "order"} {
		if strings.Contains(got, word) {
			t.Fatalf("prose word %q survived: %q", word, got)
		}
	}
	if !strings.Contains(got, "int x = 5") {
		t.Fatalf("code was lost: %q", got)
	}
}

type allowSkip struct{ toks []host.Token }

func (a *allowSkip) Keep(i int) float64 { return 4 }

func (a *allowSkip) Skip(i int) float64 {
	if a.toks[i].IsTrivia() {
		return 0
	}
	return 8
}

func TestEmptyInputDecodesToAnEmptyProgram(t *testing.T) {
	path, ok := NewMachine().Decode(nil, keepEverything{})
	if !ok {
		t.Fatal("empty input should decode")
	}
	if len(path.Actions) != 0 {
		t.Fatalf("got %d actions", len(path.Actions))
	}
}

func TestSeparatorAfterStatements(t *testing.T) {
	path, toks := decodeKeepingAll(t, "int x = 5;\nint y = 6;\n")
	semi := -1
	for i, tok := range toks {
		if tok.Text == ";" {
			semi = i
			break
		}
	}
	if semi < 0 {
		t.Fatal("no semicolon found")
	}
	if !path.SeparatorAfter[semi] {
		t.Error("a separator must be allowed after a statement terminator")
	}
}
