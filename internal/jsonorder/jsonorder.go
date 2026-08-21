// Package jsonorder recovers the order keys appeared in inside a JSON object.
//
// Go's map[string]T has discarded that by the time a decoded value exists, and
// both of the files Forge writes — schema.json and the XState behaviour
// machines — are human-authored, in version control, and read in diffs. Their
// key order carries intent: components are grouped because they belong
// together, and a machine's states are usually written in the order they run.
// Saving a file with everything alphabetised would be a whole-file diff nobody
// asked for.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Keys returns the keys of a JSON object in document order.
//
// An absent section and an explicit null both mean "no keys" rather than a
// malformed object. Treating null as an error once made LoadSchema reject a
// schema it had accepted before, which was enough to stop the engine starting.
func Keys(raw []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object, got %v", tok)
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected an object key, got %v", tok)
		}
		keys = append(keys, key)

		// Consume the value without interpreting it — only the key sequence
		// matters here, and the values are decoded properly by the caller.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// Apply returns the keys of present, arranged as recorded first and then
// anything unrecorded, sorted.
//
// One rule wherever order is preserved, so a file stays predictable. Recorded
// keys that are gone are skipped (a deleted component, a deleted state); keys
// that were never recorded are appended sorted (one added through the UI), so
// a new entry lands somewhere predictable rather than wherever a map iteration
// happened to put it.
func Apply[T any](recorded []string, present map[string]T) []string {
	out := make([]string, 0, len(present))
	seen := make(map[string]bool, len(present))

	for _, key := range recorded {
		if _, ok := present[key]; ok && !seen[key] {
			out = append(out, key)
			seen[key] = true
		}
	}

	var added []string
	for key := range present {
		if !seen[key] {
			added = append(added, key)
		}
	}
	sort.Strings(added)
	return append(out, added...)
}
