package ruby

// Vocabulary is the set of bare names that are Ruby methods or core names
// often enough to be worth telling the decoder about.
//
// The list is deliberately short. Every entry makes that word cheaper to keep
// as code, so an entry for a word that is also common in prose — "first",
// "last", "count", "size", "find", "select", "map" — would make prose harder to
// detect. Only names where the Ruby sense dominates are listed.
type Vocabulary struct{}

// NewVocabulary returns the Ruby vocabulary.
func NewVocabulary() *Vocabulary { return &Vocabulary{} }

// rubyNames are names that are effectively Ruby-specific, or so strongly
// associated with Ruby that prose rarely uses them in a code position.
var rubyNames = map[string]bool{
	// Output and input.
	"puts": true, "print": true, "p": true, "pp": true,
	"gets": true, "chomp": true, "strip": true,
	"printf": true, "sprintf": true, "format": true,
	// Loading and definition.
	"require": true, "require_relative": true, "load": true,
	"attr_reader": true, "attr_writer": true, "attr_accessor": true,
	"define_method": true, "alias_method": true, "module_function": true,
	"prepend": true, "initialize": true,
	// Functional forms.
	"lambda": true, "proc": true, "yield": true, "super": true,
	// Reflection and identity.
	"inspect": true, "freeze": true, "frozen?": true, "dup": true,
	"clone": true, "send": true, "public_send": true, "respond_to?": true,
	"nil?": true, "object_id": true, "tap": true, "then": true,
	"singleton_class": true, "instance_variable_get": true,
	"instance_variable_set": true,
	// Conversions.
	"to_s": true, "to_i": true, "to_f": true, "to_a": true, "to_h": true,
	"to_sym": true, "to_str": true, "to_proc": true, "Integer": true,
	"Float": true, "Array": true, "Hash": true, "String": true,
	// Enumerable helpers whose names are not ordinary English words.
	"each_with_index": true, "each_with_object": true,
	"group_by": true, "sort_by": true, "min_by": true, "max_by": true,
	"flat_map": true, "tally": true, "uniq": true, "flatten": true,
	"compact": true, "zip": true, "gsub": true, "sub": true,
	"join": true, "split": true,
}

// Knows implements host.Vocabulary.
func (v *Vocabulary) Knows(word string) bool { return rubyNames[word] }
