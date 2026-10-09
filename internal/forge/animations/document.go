// Package animations edits only the fields the game's animation loader reads.
// The original TOML is kept intact except for the selected field's value.
package animations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/tmbritton/ecs-db/internal/renderer"
)

type Binding struct {
	EntityType string `toml:"entity_type"`
	Sheet      string `toml:"sheet"`
}

type documentValues struct {
	Animation   []renderer.AnimDef `toml:"animation"`
	EntityAsset []Binding          `toml:"entity_asset"`
}

type Document struct {
	raw    []byte
	values documentValues
}

func Parse(data []byte) (*Document, error) {
	var values documentValues
	if _, err := toml.Decode(string(data), &values); err != nil {
		return nil, fmt.Errorf("animations.toml: %w", err)
	}
	seen := map[string]bool{}
	for _, animation := range values.Animation {
		if animation.Name == "" {
			return nil, fmt.Errorf("animation needs a name")
		}
		if seen[animation.Name] {
			return nil, fmt.Errorf("duplicate animation %q", animation.Name)
		}
		seen[animation.Name] = true
	}
	bindings := map[string]bool{}
	for _, binding := range values.EntityAsset {
		if binding.EntityType == "" {
			return nil, fmt.Errorf("entity_asset needs an entity_type")
		}
		if bindings[binding.EntityType] {
			return nil, fmt.Errorf("duplicate entity_asset for %q", binding.EntityType)
		}
		bindings[binding.EntityType] = true
	}
	return &Document{raw: bytes.Clone(data), values: values}, nil
}

func (d *Document) Bytes() []byte { return bytes.Clone(d.raw) }

func (d *Document) Animations() []renderer.AnimDef {
	copyOf := append([]renderer.AnimDef(nil), d.values.Animation...)
	for i := range copyOf {
		copyOf[i].Frames = append([]int(nil), copyOf[i].Frames...)
	}
	return copyOf
}

func (d *Document) Bindings() []Binding { return append([]Binding(nil), d.values.EntityAsset...) }

func (d *Document) SetFPS(name string, fps float64) error {
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return fmt.Errorf("animation %q needs a positive finite fps", name)
	}
	return d.replaceField("animation", name, "fps", strconv.FormatFloat(fps, 'f', -1, 64))
}

func (d *Document) SetFrames(name string, frames []int) error {
	if len(frames) == 0 {
		return fmt.Errorf("animation %q needs at least one frame", name)
	}
	parts := make([]string, len(frames))
	for i, frame := range frames {
		if frame < 0 {
			return fmt.Errorf("animation %q has negative frame %d", name, frame)
		}
		parts[i] = strconv.Itoa(frame)
	}
	return d.replaceField("animation", name, "frames", "["+strings.Join(parts, ", ")+"]")
}

func (d *Document) SetLoop(name string, loop bool) error {
	return d.replaceField("animation", name, "loop", strconv.FormatBool(loop))
}

func (d *Document) SetSheet(name, sheet string) error {
	literal, err := tomlString(sheet)
	if err != nil {
		return err
	}
	return d.replaceField("animation", name, "sheet", literal)
}

func (d *Document) SetBindingSheet(entityType, sheet string) error {
	literal, err := tomlString(sheet)
	if err != nil {
		return err
	}
	return d.replaceField("entity_asset", entityType, "sheet", literal)
}

func (d *Document) RenameAnimation(from, to string) error {
	literal, err := tomlString(to)
	if err != nil {
		return err
	}
	return d.replaceField("animation", from, "name", literal)
}

func (d *Document) RenameBinding(from, to string) error {
	literal, err := tomlString(to)
	if err != nil {
		return err
	}
	return d.replaceField("entity_asset", from, "entity_type", literal)
}

func (d *Document) AddAnimation(name, sheet string, frames []int, fps float64, loop bool) error {
	if name == "" || sheet == "" || len(frames) == 0 || fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return fmt.Errorf("animation needs a name, sheet, frames and positive finite fps")
	}
	for _, frame := range frames {
		if frame < 0 {
			return fmt.Errorf("animation %q has negative frame %d", name, frame)
		}
	}
	n, err := tomlString(name)
	if err != nil {
		return err
	}
	s, err := tomlString(sheet)
	if err != nil {
		return err
	}
	parts := make([]string, len(frames))
	for i, frame := range frames {
		parts[i] = strconv.Itoa(frame)
	}
	addition := "\n[[animation]]\nname = " + n + "\nsheet = " + s +
		"\nframes = [" + strings.Join(parts, ", ") + "]\nfps = " + strconv.FormatFloat(fps, 'f', -1, 64) +
		"\nloop = " + strconv.FormatBool(loop) + "\n"
	return d.appendSection(addition)
}

func (d *Document) AddBinding(entityType, sheet string) error {
	if entityType == "" || sheet == "" {
		return fmt.Errorf("entity_asset needs an entity type and sheet")
	}
	name, err := tomlString(entityType)
	if err != nil {
		return err
	}
	s, err := tomlString(sheet)
	if err != nil {
		return err
	}
	return d.appendSection("\n[[entity_asset]]\nentity_type = " + name + "\nsheet = " + s + "\n")
}

func (d *Document) appendSection(text string) error {
	next := append(bytes.Clone(d.raw), text...)
	parsed, err := Parse(next)
	if err != nil {
		return err
	}
	d.raw, d.values = parsed.raw, parsed.values
	return nil
}

func tomlString(v string) (string, error) {
	if !utf8.ValidString(v) {
		return "", fmt.Errorf("TOML string is not UTF-8")
	}
	quoted, _ := json.Marshal(v) // all Go strings are JSON-encodable
	return string(quoted), nil
}

type line struct {
	start int
	text  string
}

type section struct {
	kind       string
	start, end int // line indexes, header inclusive, next header exclusive
}

// tomlLines includes the original newline in each line so byte offsets stay
// stable for CRLF, comments and unknown fields.
func tomlLines(data []byte) []line {
	var out []line
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, line{start: start, text: string(data[start : i+1])})
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, line{start: start, text: string(data[start:])})
	}
	return out
}

func (d *Document) replaceField(kind, identity, key, literal string) error {
	lines := tomlLines(d.raw)
	sections := tomlSections(lines, d.raw)
	var animationSections, bindingSections int
	for _, entry := range sections {
		switch entry.kind {
		case "animation":
			animationSections++
		case "entity_asset":
			bindingSections++
		}
	}
	if animationSections != len(d.values.Animation) || bindingSections != len(d.values.EntityAsset) {
		return fmt.Errorf("cannot safely locate all TOML array tables for editing")
	}
	var index int
	for _, entry := range sections {
		if entry.kind != kind {
			continue
		}
		var current string
		if kind == "animation" && index < len(d.values.Animation) {
			current = d.values.Animation[index].Name
		} else if kind == "entity_asset" && index < len(d.values.EntityAsset) {
			current = d.values.EntityAsset[index].EntityType
		}
		index++
		if current != identity {
			continue
		}
		multiline := byte(0)
		for i := entry.start + 1; i < entry.end; i++ {
			text := lines[i].text
			if multiline != 0 {
				multiline = nextMultiline(text, multiline)
				continue
			}
			multiline = nextMultiline(text, multiline)
			trimmed := strings.TrimLeft(text, " \t")
			eq := strings.IndexByte(trimmed, '=')
			if eq < 0 || tomlFieldKey(strings.TrimSpace(trimmed[:eq])) != key {
				continue
			}
			begin := len(text) - len(trimmed) + eq + 1
			for begin < len(text) && (text[begin] == ' ' || text[begin] == '\t') {
				begin++
			}
			end := tomlCommentStart(text, begin)
			for end > begin && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\n' || text[end-1] == '\r') {
				end--
			}
			startOffset := lines[i].start + begin
			endOffset := lines[i].start + end
			if begin < len(text) && text[begin] == '[' {
				var err error
				endOffset, err = arrayValueEnd(d.raw, startOffset)
				if err != nil {
					return fmt.Errorf("animation %q has an unlocatable %s array: %w", identity, key, err)
				}
			} else if begin+3 <= len(text) && (strings.HasPrefix(text[begin:], `"""`) || strings.HasPrefix(text[begin:], `'''`)) {
				var err error
				endOffset, err = multilineValueEnd(d.raw, startOffset)
				if err != nil {
					return fmt.Errorf("animation %q has an unlocatable %s string: %w", identity, key, err)
				}
			} else if end == begin {
				return fmt.Errorf("animation %q has an empty %s value", identity, key)
			}
			next := make([]byte, 0, len(d.raw)+len(literal))
			next = append(next, d.raw[:startOffset]...)
			next = append(next, literal...)
			next = append(next, d.raw[endOffset:]...)
			parsed, err := Parse(next)
			if err != nil {
				return err // the held bytes remain intact on a refused edit
			}
			d.raw, d.values = parsed.raw, parsed.values
			return nil
		}
		at := len(d.raw)
		if entry.end < len(lines) {
			at = lines[entry.end].start
		}
		prefix := ""
		if at > 0 && d.raw[at-1] != '\n' {
			prefix = "\n"
		}
		addition := prefix + key + " = " + literal + "\n"
		next := make([]byte, 0, len(d.raw)+len(addition))
		next = append(next, d.raw[:at]...)
		next = append(next, addition...)
		next = append(next, d.raw[at:]...)
		parsed, err := Parse(next)
		if err != nil {
			return err
		}
		d.raw, d.values = parsed.raw, parsed.values
		return nil
	}
	return fmt.Errorf("%s %q is not in animations.toml", kind, identity)
}

// arrayValueEnd skips comments while finding the closing bracket. The TOML
// decoder has already checked the array's syntax; frames contains only integers.
func arrayValueEnd(raw []byte, start int) (int, error) {
	depth, comment := 0, false
	var quote byte
	for i := start; i < len(raw); i++ {
		if quote != 0 {
			if raw[i] == quote {
				quote = 0
			} else if quote == '"' && raw[i] == '\\' {
				i++
			}
			continue
		}
		switch raw[i] {
		case '\n':
			comment = false
		case '#':
			if !comment {
				comment = true
			}
		case '"', '\'':
			if !comment {
				quote = raw[i]
			}
		case '[':
			if !comment {
				depth++
			}
		case ']':
			if !comment {
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("missing closing bracket")
}

func multilineValueEnd(raw []byte, start int) (int, error) {
	delimiter := raw[start : start+3]
	for i := start + 3; i+3 <= len(raw); i++ {
		if delimiter[0] == '"' && raw[i] == '\\' {
			i++
			continue
		}
		if bytes.Equal(raw[i:i+3], delimiter) {
			// TOML permits one or two quote characters as the final
			// characters of a multiline value. They share a contiguous
			// run with its three closing quotes.
			end := i + 3
			for end < len(raw) && end-i < 5 && raw[end] == delimiter[0] {
				end++
			}
			return end, nil
		}
	}
	return 0, fmt.Errorf("missing closing quotes")
}

func tomlFieldKey(raw string) string {
	if len(raw) < 2 {
		return raw
	}
	if raw[0] == '"' && raw[len(raw)-1] == '"' {
		if decoded, err := strconv.Unquote(raw); err == nil {
			return decoded
		}
	}
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1]
	}
	return raw
}

// tomlSections ignores apparent headers inside multiline TOML strings.
func tomlSections(lines []line, raw []byte) []section {
	var sections []section
	multiline := byte(0)
	arrayThrough := 0
	for i, entry := range lines {
		if multiline == 0 && entry.start >= arrayThrough {
			trimmed := strings.TrimSpace(entry.text)
			if tomlHeaderLine(trimmed) {
				if n := len(sections); n > 0 {
					sections[n-1].end = i
				}
				kind := tomlHeaderKind(trimmed)
				sections = append(sections, section{kind: kind, start: i, end: len(lines)})
			} else if eq := strings.IndexByte(trimmed, '='); eq > 0 && trimmed[0] != '#' {
				value := strings.TrimLeft(trimmed[eq+1:], " \t")
				if strings.HasPrefix(value, "[") {
					start := entry.start + strings.Index(entry.text, value)
					if end, err := arrayValueEnd(raw, start); err == nil {
						arrayThrough = end
					}
				}
			}
		}
		multiline = nextMultiline(entry.text, multiline)
	}
	return sections
}

func tomlHeaderLine(text string) bool {
	if !strings.HasPrefix(text, "[") {
		return false
	}
	if strings.HasPrefix(text, "[[") {
		close := strings.Index(text, "]]")
		return close > 2 && headerEnds(text[close+2:])
	}
	close := strings.IndexByte(text, ']')
	return close > 1 && headerEnds(text[close+1:])
}

func tomlHeaderKind(header string) string {
	if !strings.HasPrefix(header, "[[") {
		return "other"
	}
	close := strings.Index(header[2:], "]]")
	if close < 0 {
		return "other"
	}
	key := strings.TrimSpace(header[2 : close+2])
	if !headerEnds(header[close+4:]) {
		return "other"
	}
	if len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"' {
		decoded, err := strconv.Unquote(key)
		if err != nil {
			return "other"
		}
		key = decoded
	} else if len(key) >= 2 && key[0] == '\'' && key[len(key)-1] == '\'' {
		key = key[1 : len(key)-1]
	}
	if key == "animation" || key == "entity_asset" {
		return key
	}
	return "other"
}

func headerEnds(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

// nextMultiline tracks triple-quoted strings. A header-shaped line in one is
// data, not a table; ordinary quoted strings and comments cannot open one.
func nextMultiline(text string, state byte) byte {
	var quoted byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if state != 0 {
			if c == state && strings.HasPrefix(text[i:], strings.Repeat(string(state), 3)) {
				state = 0
				i += 2
			} else if state == '"' && c == '\\' {
				i++
			}
			continue
		}
		if quoted != 0 {
			if c == quoted {
				quoted = 0
			} else if quoted == '"' && c == '\\' {
				i++
			}
			continue
		}
		if c == '#' {
			break
		}
		switch c {
		case '"', '\'':
			if strings.HasPrefix(text[i:], strings.Repeat(string(c), 3)) {
				state = c
				i += 2
			} else {
				quoted = c
			}
		}
	}
	return state
}

func tomlCommentStart(text string, start int) int {
	var quote byte
	for i := start; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if quote == '"' && c == '\\' {
				i++
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '#':
			return i
		}
	}
	return len(text)
}
