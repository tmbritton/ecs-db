package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// keyOrder returns the keys of a JSON object in the order they appear in the
// document. Go's map[string]T has discarded that by the time a value exists,
// so it has to be read from the raw bytes.
func keyOrder(raw []byte) ([]string, error) {
	// An absent section and an explicit null both mean "no keys". Treating null
	// as a malformed object made LoadSchema reject schemas it accepted before —
	// including a valid one with "properties": null, which would have stopped
	// the engine from starting.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
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
		// matters here, and the values are decoded properly elsewhere.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// orderedKeys returns the keys of present, arranged as recorded first and then
// anything unrecorded, sorted.
//
// One rule at every level — components, the properties inside them, and entity
// types — so the file stays predictable. Recorded keys that are gone are
// skipped (a deleted component); keys that were never recorded are appended
// sorted (one added through the UI), so a new component lands somewhere
// predictable rather than wherever a map iteration happened to put it.
func orderedKeys[T any](recorded []string, present map[string]T) []string {
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
