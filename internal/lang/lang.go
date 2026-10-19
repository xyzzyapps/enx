// Package lang registers every host language the tool knows about.
//
// It is the single place that names concrete lexers and grammar machines, so
// that both the command line and the tests see the same set.
package lang

import (
	"enx/internal/host"
	"enx/internal/host/c"
	"enx/internal/host/perl"
	"enx/internal/host/ruby"
	"enx/internal/registry"
)

// New returns a registry with every known language registered.
func New() *registry.Registry {
	reg := registry.New()

	reg.Register(host.Spec{
		Language:      "ruby",
		Extension:     ".rb",
		CommentPrefix: "#",
		Interpreter:   []string{"ruby"},
		Lexer:         ruby.New(),
		Machine:       ruby.NewMachine(),
		Vocabulary:    ruby.NewVocabulary(),
		Types:         ruby.NewTypes(),
	})

	reg.Register(host.Spec{
		Language:      "perl",
		Extension:     ".pl",
		CommentPrefix: "#",
		Interpreter:   []string{"perl"},
		Lexer:         perl.New(),
		Machine:       perl.NewMachine(),
		Vocabulary:    perl.NewVocabulary(),
		Types:         perl.NewTypes(),
	})

	reg.Register(host.Spec{
		Language:      "c",
		Extension:     ".c",
		CommentPrefix: "//",
		Compile:       []string{"gcc", "-std=c11", "-O0", "-o", "{out}", "{src}"},
		Lexer:         c.New(),
		Machine:       c.NewMachine(),
		Vocabulary:    c.NewVocabulary(),
		Types:         c.NewTypes(),
	})

	return reg
}
