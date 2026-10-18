package view

import (
	"net/url"
	"strconv"
)

// FormState carries a form shown again after a failed post: the posted
// values and one message per field. Key names a form on pages with several
// (only that one gets the values back and opens).
type FormState struct {
	Key    string
	Values map[string]string
	Errors map[string]string
}

// For returns the state when it belongs to form key, an empty state otherwise.
func (s *FormState) For(key string) *FormState {
	if s == nil || s.Key != key {
		return &FormState{}
	}
	return s
}

// Posted reports whether this form comes back from a failed post.
func (s *FormState) Posted() bool { return s != nil && s.Values != nil }

// V is the posted value of name, or def when the form was not posted.
func (s *FormState) V(name, def string) string {
	if s.Posted() {
		return s.Values[name]
	}
	return def
}

// Checked is the posted checkbox state, or def.
func (s *FormState) Checked(name string, def bool) bool {
	if s.Posted() {
		return s.Values[name] != ""
	}
	return def
}

// E is the message of a field ("" when fine).
func (s *FormState) E(name string) string {
	if s == nil {
		return ""
	}
	return s.Errors[name]
}

// Field describes one input.
type Field struct {
	Name, Label, Hint string
	Type              string // text (default), email, tel, url, date, number
	Dir               string // ltr / rtl for language-specific fields
	Mode              string // inputmode
	Max               int    // maxlength (0: none)
	Required          bool
	Rows              int // > 0: textarea
	Placeholder       string
	Class             string
}

func (f Field) id(prefix string) string { return prefix + "-" + f.Name }

func (f Field) inputType() string {
	if f.Type == "" {
		return "text"
	}
	return f.Type
}

func maxAttr(n int) string { return strconv.Itoa(n) }

// Section is one card of a settings-like page.
const adminCard = "rounded-card bg-white p-g2 shadow-lift ring-1 ring-sand-deep/50 sm:p-g3"

func urlQuery(s string) string { return url.QueryEscape(s) }

func int32String(n int32) string { return strconv.FormatInt(int64(n), 10) }

func modeOf(auto bool) string {
	if auto {
		return "auto"
	}
	return "fixed"
}
