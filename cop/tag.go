package cop

import (
	"reflect"
	"slices"
	"strings"
)

// TagOptions describes the contents of a single struct-tag entry, such as
// `json:"name,omitempty"`.
//
// Name is the first comma-separated token (empty when the tag value starts
// with a comma, as in `json:",inline"`); Options is everything after it.
//
// Use [ParseTagOptions] to construct a TagOptions from a [reflect.StructTag],
// then call Has / HasAny to query individual modifiers without re-implementing
// comma-splitting in every cop.
type TagOptions struct {
	Name    string
	Options []string
}

// Has reports whether opt is present in Options.
func (t TagOptions) Has(opt string) bool {
	return slices.Contains(t.Options, opt)
}

// HasAny reports whether at least one of opts is present in Options.
//
//	jsonOpts.HasAny("omitempty", "omitzero")
func (t TagOptions) HasAny(opts ...string) bool {
	return slices.ContainsFunc(opts, t.Has)
}

// IsSkipped reports whether the tag explicitly opts out (Name == "-"),
// the JSON / YAML convention for "omit this field from the wire format".
func (t TagOptions) IsSkipped() bool { return t.Name == "-" }

// HasName reports whether the tag carries a usable name — non-empty and
// not the explicit-skip sentinel "-". Use it when iterating struct fields
// to discard tags that don't contribute to the wire format:
//
//	opts, ok := cop.ParseTagOptions(tag, "json")
//	if !ok || !opts.HasName() { continue }
func (t TagOptions) HasName() bool { return t.Name != "" && !t.IsSkipped() }

// ParseTagOptions reads the value of the given key from a struct tag and
// splits it into Name + Options. The second result is false when the tag
// does not contain key.
//
//	tag := reflect.StructTag(`json:"x,omitempty" yaml:",inline"`)
//	j, _ := cop.ParseTagOptions(tag, "json") // {Name: "x", Options: ["omitempty"]}
//	y, _ := cop.ParseTagOptions(tag, "yaml") // {Name: "",  Options: ["inline"]}
func ParseTagOptions(tag reflect.StructTag, key string) (TagOptions, bool) {
	raw, ok := tag.Lookup(key)
	if !ok {
		return TagOptions{}, false
	}
	parts := strings.Split(raw, ",")
	return TagOptions{Name: parts[0], Options: parts[1:]}, true
}
