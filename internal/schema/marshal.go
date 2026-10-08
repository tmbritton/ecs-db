package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Marshal serialises a schema back to schema.json.
//
// It is hand-rolled rather than encoding/json on the struct, for two reasons
// the struct cannot express. Order: Go maps have none, so json.Marshal emits
// keys alphabetically and the first save reorders a file nobody meant to
// reorder. And layout: the authored file writes properties inline with their
// values column-aligned, which is a real part of how it reads.
//
// The formatting rules are exactly what the checked-in schema.json uses, so a
// load-marshal round trip is byte-stable and a save diffs only what changed:
//
//   - two-space indent, trailing newline
//   - a property renders inline — { "type": "number" } — when it has no nested
//     properties or items of its own
//   - inside one properties block, keys are padded so every value starts at the
//     same column. Only there: entity-type fields and component fields take a
//     single space, as the authored file does.
//   - arrays of component names render inline
//
// Marshal is not validation. It writes what it is given, because the caller may
// be halfway through an edit; ValidateSchema is a separate call the save path
// makes first.
func Marshal(s DatabaseSchema) ([]byte, error) {
	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"schemaVersion\": %d,\n", s.SchemaVersion)

	b.WriteString("  \"components\": {\n")
	names := orderedKeys(s.ComponentOrder, s.Components)
	for i, name := range names {
		if err := writeComponent(&b, name, s.Components[name], i == len(names)-1); err != nil {
			return nil, err
		}
	}
	b.WriteString("  },\n")

	b.WriteString("  \"entityTypes\": {\n")
	types := orderedKeys(s.EntityTypeOrder, s.EntityTypes)
	for i, name := range types {
		if err := writeEntityType(&b, name, s.EntityTypes[name], i == len(types)-1); err != nil {
			return nil, err
		}
	}
	b.WriteString("  }")
	if len(s.Interactions) > 0 {
		b.WriteString(",\n  \"interactions\": {\n")
		components := orderedKeys(s.InteractionOrder, s.Interactions)
		for i, name := range components {
			component, err := jsonString(name)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "    %s: {\n", component)
			categories := orderedKeys(s.InteractionCategoryOrder[name], s.Interactions[name])
			for j, category := range categories {
				key, err := jsonString(category)
				if err != nil {
					return nil, err
				}
				rule := s.Interactions[name][category]
				allows := rule.Allows
				if allows == nil {
					allows = []string{}
				}
				choices, err := json.Marshal(allows)
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(&b, "      %s: { \"open\": %t, \"allows\": %s }", key, rule.Open, choices)
				if j != len(categories)-1 {
					b.WriteString(",")
				}
				b.WriteString("\n")
			}
			b.WriteString("    }")
			if i != len(components)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  }")
	}
	b.WriteString("\n")

	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeComponent(b *strings.Builder, name string, c Component, last bool) error {
	key, err := jsonString(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "    %s: {\n", key)

	typ, err := jsonString(c.Type)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "      \"type\": %s", typ)

	if c.RenamedFrom != "" {
		from, err := jsonString(c.RenamedFrom)
		if err != nil {
			return err
		}
		b.WriteString(",\n")
		fmt.Fprintf(b, "      \"renamedFrom\": %s", from)
	}

	if c.Behavior != "" {
		behavior, err := jsonString(c.Behavior)
		if err != nil {
			return err
		}
		b.WriteString(",\n")
		fmt.Fprintf(b, "      \"behavior\": %s", behavior)
	}

	// Independent, not alternatives. Nothing rejects a component carrying both
	// — UnmarshalJSON checks that an object has properties and an array has
	// items, never that the other is absent — so a switch here silently dropped
	// one of them and wrote a file the loader then refused.
	if len(c.Properties) > 0 {
		b.WriteString(",\n      \"properties\": {\n")
		if err := writeProperties(b, "        ", c.PropertyOrder, c.Properties); err != nil {
			return err
		}
		b.WriteString("      }")
	}
	if c.Items != nil {
		items, err := propertyValue(*c.Items, "      ")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, ",\n      \"items\": %s", items)
	}

	b.WriteString("\n    }")
	if !last {
		b.WriteString(",")
	}
	b.WriteString("\n")
	return nil
}

// writeProperties emits one properties block with its keys column-aligned, the
// way the authored file arranges them.
func writeProperties(b *strings.Builder, indent string, order []string, props map[string]Property) error {
	names := orderedKeys(order, props)

	// Pad to the longest key so every value starts at the same column. Computed
	// per block, which is what makes it deterministic rather than a matter of
	// whoever last edited the file by hand.
	width := 0
	keys := make(map[string]string, len(names))
	for _, name := range names {
		key, err := jsonString(name)
		if err != nil {
			return err
		}
		keys[name] = key
		if len(key) > width {
			width = len(key)
		}
	}

	for i, name := range names {
		value, err := propertyValue(props[name], indent)
		if err != nil {
			return err
		}
		padding := strings.Repeat(" ", width-len(keys[name]))
		fmt.Fprintf(b, "%s%s:%s %s", indent, keys[name], padding, value)
		if i != len(names)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	return nil
}

// propertyValue renders one property. A leaf — no nested properties, no items —
// renders inline, which is how the authored file writes the overwhelming
// majority of them. Anything richer expands, because inlining it would produce
// a line nobody can read.
func propertyValue(p Property, indent string) (string, error) {
	typ, err := jsonString(p.Type)
	if err != nil {
		return "", err
	}
	// renamedFrom stays on the inline form. It is one short key, and expanding
	// a leaf property over four lines because it carries one would reformat
	// most of the file the first time anybody renames anything.
	if len(p.Properties) == 0 && p.Items == nil {
		out := "{ \"type\": " + typ
		if p.RenamedFrom != "" {
			from, err := jsonString(p.RenamedFrom)
			if err != nil {
				return "", err
			}
			out += ", \"renamedFrom\": " + from
		}
		return out + " }", nil
	}

	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "%s  \"type\": %s", indent, typ)
	if p.RenamedFrom != "" {
		from, err := jsonString(p.RenamedFrom)
		if err != nil {
			return "", err
		}
		b.WriteString(",\n")
		fmt.Fprintf(&b, "%s  \"renamedFrom\": %s", indent, from)
	}
	// Independent, for the same reason as writeComponent's.
	if len(p.Properties) > 0 {
		b.WriteString(",\n")
		fmt.Fprintf(&b, "%s  \"properties\": {\n", indent)
		if err := writeProperties(&b, indent+"    ", p.PropertyOrder, p.Properties); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  }", indent)
	}
	if p.Items != nil {
		items, err := propertyValue(*p.Items, indent+"  ")
		if err != nil {
			return "", err
		}
		b.WriteString(",\n")
		fmt.Fprintf(&b, "%s  \"items\": %s", indent, items)
	}
	fmt.Fprintf(&b, "\n%s}", indent)
	return b.String(), nil
}

func writeEntityType(b *strings.Builder, name string, et EntityType, last bool) error {
	key, err := jsonString(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "    %s: {\n", key)

	required, err := stringArray(et.RequiredComponents)
	if err != nil {
		return err
	}
	optional, err := stringArray(et.OptionalComponents)
	if err != nil {
		return err
	}
	level, err := jsonString(string(et.ValidationLevel))
	if err != nil {
		return err
	}

	// What this type used to be called, when it was renamed. First, because it
	// is a fact about the type itself rather than part of its definition.
	if et.RenamedFrom != "" {
		from, err := jsonString(et.RenamedFrom)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "      \"renamedFrom\": %s,\n", from)
	}

	// The binding to a state machine. Omitted when empty, like the component's
	// own behavior field, and emitted before the component lists because that is
	// where the authored files put it.
	if et.Behavior != "" {
		behavior, err := jsonString(et.Behavior)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "      \"behavior\": %s,\n", behavior)
	}

	// Emitted unconditionally and never as null: EntityType has no omitempty on
	// its slices, so encoding/json would write "optionalComponents": null for a
	// type that declares none, and null is not a list of component names.
	fmt.Fprintf(b, "      \"requiredComponents\": %s,\n", required)
	fmt.Fprintf(b, "      \"optionalComponents\": %s,\n", optional)
	fmt.Fprintf(b, "      \"allowExtraComponents\": %t,\n", et.AllowExtraComponents)
	fmt.Fprintf(b, "      \"validationLevel\": %s\n", level)

	b.WriteString("    }")
	if !last {
		b.WriteString(",")
	}
	b.WriteString("\n")
	return nil
}

// stringArray renders a list of component names inline, as the authored file
// does. A nil slice is an empty array, not null.
func stringArray(values []string) (string, error) {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		q, err := jsonString(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, q)
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

// jsonString quotes a string the way encoding/json would, with HTML escaping
// off. The default turns <, > and & into < and friends, which changes
// nothing semantically and would make the diff unreadable for any name
// containing them.
func jsonString(s string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
