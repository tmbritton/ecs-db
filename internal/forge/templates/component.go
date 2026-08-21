package templates

import "github.com/a-h/templ"

// Component aliases templ.Component so the server package does not need to
// import templ directly.
type Component = templ.Component

// URL and kv let .templ files use templ's URL and conditional-class helpers
// without importing templ, which collides with the `templ` identifier templ's
// own generated code declares in the same package.
//
// This wraps templ.URL, which sanitizes — NOT templ.SafeURL, which is a bare
// type conversion asserting the string is already trusted. The only href in
// the package today is built from a table slug, but this is the idiom every
// later mode will copy, and the difference between the two is invisible at the
// call site.
func URL(s string) templ.SafeURL { return templ.URL(s) }

func kv(class string, on bool) templ.KeyValue[string, bool] { return templ.KV(class, on) }
