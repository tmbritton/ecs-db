package modes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
)

// ActionMeta and ParamSchema are aliased for the same reason every other type
// in this package is: a .templ file here cannot import agent without colliding
// with templ's codegen.
type (
	ActionMeta  = agent.ActionMeta
	ParamSchema = agent.ParamSchema
	ActionSpec  = agent.ActionSpec
)

// selectedStatePath is the dotted path the canvas selection names, or "" when
// nothing or an edge is selected.
func selectedStatePath(data Data) string {
	if !strings.HasPrefix(data.Chart.Selected, chart.SelState) {
		return ""
	}
	return strings.TrimPrefix(data.Chart.Selected, chart.SelState)
}

// stateActions is a state's entry or exit list, in authored order — which is
// the order they run in, so sorting them would be sorting the logic.
func stateActions(data Data, kind string) []agent.ActionSpec {
	if data.SelectedState == nil {
		return nil
	}
	if kind == "exit" {
		return data.SelectedState.Exit
	}
	return data.SelectedState.Entry
}

// actionDescription is what the registry says an action does. Empty for one
// the registry does not have, which is a machine written by hand naming
// something the engine will refuse — the problem list says so, and the
// inspector does not pretend to know what it does.
func actionDescription(data Data, name string) string {
	for _, meta := range data.Actions {
		if meta.Name == name {
			return meta.Description
		}
	}
	return ""
}

// actionParams is what an action takes, from the registry.
func actionParams(data Data, name string) []ParamSchema {
	for _, meta := range data.Actions {
		if meta.Name == name {
			return meta.Params
		}
	}
	return nil
}

// paramValue is what the file holds for one parameter, as a form field shows it.
//
// The value only. A registered default is a placeholder, never a value: writing
// it in would put the default into the file as though someone had chosen it,
// and there would then be no way to say "leave this alone".
func paramValue(spec agent.ActionSpec, name string) string {
	v, ok := spec.Params[name]
	if !ok {
		return ""
	}
	return formatParam(v)
}

// formatParam is a parameter's value as a form field shows it, and as the
// field will hand it back.
//
// The round trip is the whole requirement. An object was falling through to
// %v, which prints a Go map as map[hp:3 max:10] — so the textarea showed
// something that had never been JSON, and touching an object parameter at all
// refused with "invalid character 'm'". The only way to edit one was to select
// all and retype it, with nothing on screen saying the text was not the value.
func formatParam(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// 'f' and not %v: a magnitude that %v prints as 1e+06 is not a number a
		// JSON file holds, and it would come back as one.
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			// Nothing the parser produces can fail here, and a Go rendering is
			// no worse than nothing when it does.
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}

// paramPlaceholder is the registered default, shown as a hint.
func paramPlaceholder(p ParamSchema) string {
	if p.Default == nil {
		return ""
	}
	return formatParam(p.Default)
}

// paramInputType maps the registry's declared type onto a form control.
//
// "object" is a textarea holding JSON, decided here rather than when
// attachComponent's data parameter forces it at an awkward moment: the engine
// takes an arbitrary object there, and pretending otherwise would mean
// inventing a schema the registry does not have.
// paramInputType maps the registry's declared type onto a form control.
//
// A number is a *text* field, deliberately, and that is not laziness. The value
// of an `input type=number` is the empty string whenever its contents are not a
// valid float — so typing "1e" on the way to "1e5" and tabbing out posted an
// empty value, which cleared the parameter that was already there. The refusal
// message convert() carries for a number that will not parse was unreachable
// for the same reason: the browser never let the bad text out.
//
// inputmode keeps the numeric keypad on a touch device without the empty-value
// rule coming with it.
func paramInputType(p ParamSchema) string {
	if p.Type == "boolean" {
		return "checkbox"
	}
	return "text"
}

func paramInputMode(p ParamSchema) string {
	if p.Type == "number" {
		return "decimal"
	}
	return ""
}

// paramHint is the registered default for a control that cannot show a
// placeholder — a checkbox.
//
// A default is a hint and never a value, so a boolean parameter that is absent
// renders unchecked while the engine would use its default. Saying so beats a
// box whose state contradicts what would happen.
func paramHint(p ParamSchema) string {
	if p.Type != "boolean" || p.Default == nil {
		return ""
	}
	return "default " + formatParam(p.Default)
}

func paramIsObject(p ParamSchema) bool { return p.Type == "object" }

func paramIsChecked(spec agent.ActionSpec, name string) bool {
	v, ok := spec.Params[name]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// paramProblems is what is wrong with one parameter right now.
//
// A warning and never blocking, which is a deliberate limit. ValidateMachine
// checks that an action is registered and stops; it does not look at
// parameters. So "required and empty" is Forge's rule, not the engine's, and
// blocking the save for it would be inventing a rule the engine does not have —
// the same line internal/forge/validation draws for the ambiguity warning.
func paramProblems(spec agent.ActionSpec, p ParamSchema) []components.Problem {
	if !p.Required || paramValue(spec, p.Name) != "" {
		return nil
	}
	return []components.Problem{{
		Field:   p.Name,
		Message: p.Name + " is required — the machine still loads, and the action fails when it runs.",
	}}
}

func paramProblemsID(kind string, index int, param string) string {
	return components.ProblemsID("action", kind, strconv.Itoa(index), param)
}

// initialLabel says what "set as initial" would do, naming the container it
// acts on — a nested state's initial belongs to the compound state that holds
// it, and the machine's is a different field.
func initialLabel(data Data) string {
	path := selectedStatePath(data)
	if i := strings.LastIndex(path, "."); i >= 0 {
		return "Make this the state " + path[:i] + " enters"
	}
	return "Make this the state the machine starts in"
}

// isCurrentInitial reports whether the selected state is already the one its
// container enters, in which case the control is absent rather than disabled:
// an action that does nothing is worse than an action that is not there.
func isCurrentInitial(data Data) bool {
	path := selectedStatePath(data)
	if path == "" || data.Machine == nil {
		return false
	}
	name, parent := path, ""
	if i := strings.LastIndex(path, "."); i >= 0 {
		name, parent = path[i+1:], path[:i]
	}
	if parent == "" {
		return data.Machine.Initial == name
	}
	node, err := stateNodeAt(data.Machine, parent)
	if err != nil {
		return false
	}
	return node.Initial == name
}

// stateName is the leaf of the selected path, which is what the rename edits.
func stateName(data Data) string { return shortName(selectedStatePath(data)) }

// actionOptions is the catalogue as a dropdown. Every registered action, every
// time: the same action twice on one state is legitimate, so nothing is removed
// from the list once it has been used.
func actionOptions(data Data) []components.Option {
	out := make([]components.Option, 0, len(data.Actions)+1)
	out = append(out, components.Option{Value: "", Label: "+ add action…"})
	for _, meta := range data.Actions {
		// The description travels with the name, so what an action does is
		// there while you are choosing rather than only after you have chosen.
		// A native select shows the whole label in its popup and the closed
		// control truncates, which is exactly the behaviour wanted in a rail
		// this narrow.
		label := meta.Name
		if meta.Description != "" {
			label += " — " + meta.Description
		}
		out = append(out, components.Option{Value: meta.Name, Label: label})
	}
	return out
}

// addActionAction posts the chosen action for the selected state.
func addActionAction(data Data, kind string) string {
	return "@post('/forge/agents/action?machine=" + urlValue(data.SelectedMachine) +
		"&state=" + urlValue(selectedStatePath(data)) + "&kind=" + urlValue(kind) +
		"&add=' + encodeURIComponent(evt.target.value))"
}

func removeActionAction(data Data, kind string, index int) string {
	return action("/forge/agents/action",
		"machine", data.SelectedMachine, "state", selectedStatePath(data),
		"kind", kind, "remove", strconv.Itoa(index))
}

// paramAction posts a parameter's value as the control that changed it sees it.
func paramAction(data Data, kind string, index int, param string, checkbox bool) string {
	base := "/forge/agents/action?machine=" + urlValue(data.SelectedMachine) +
		"&state=" + urlValue(selectedStatePath(data)) + "&kind=" + urlValue(kind) +
		"&index=" + strconv.Itoa(index) + "&param=" + urlValue(param) + "&value="
	if checkbox {
		// A checkbox has no useful value attribute; what it means is whether it
		// is checked.
		return "@post('" + base + "' + evt.target.checked)"
	}
	return "@post('" + base + "' + encodeURIComponent(evt.target.value))"
}

func renameStateAction(data Data) string {
	return valueAction("/forge/agents/state", "to",
		"machine", data.SelectedMachine, "rename", selectedStatePath(data))
}

func setInitialAction(data Data) string {
	return action("/forge/agents/state",
		"machine", data.SelectedMachine, "initial", selectedStatePath(data))
}

func deleteStateAction(data Data) string {
	return confirmAction(data.SelectedStateWarning,
		action("/forge/agents/state",
			"machine", data.SelectedMachine, "delete", selectedStatePath(data)))
}

// stateNodeAt resolves a dotted path exactly, the same rule the mutations use —
// and the same implementation, rather than a second walk of the tree here.
func stateNodeAt(def *MachineDefinition, path string) (*agent.StateNode, error) {
	return machines.StateAt(def, path)
}
