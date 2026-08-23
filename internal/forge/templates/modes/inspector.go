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

// ParamForm is one generated parameter form: what the registry declares, what
// the file holds, and where a change goes.
//
// A struct rather than four arguments because there are four callers — a
// state's entry actions, a state's exit actions, a transition's actions and a
// transition's guard — and each of them builds it from something different.
// Scope is what makes the test ids and the problem ids unique, and keeping
// "entry-0" as a scope is what keeps Story 6's ids exactly as they were.
type ParamForm struct {
	Scope string
	// What the form is generating for, in words — "action" or "guard". It ends
	// up in the required-parameter message, which said "the action fails when
	// it runs" under a guard's parameters once the form gained a second kind of
	// caller. The form was generalised; its copy has to be too.
	What   string
	Params []ParamSchema
	Values map[string]any
	// Post builds the Datastar expression that sends one parameter's new value.
	// A checkbox reports what it means through `checked` rather than `value`,
	// which is the only reason the caller has to know which control it is.
	Post func(param string, checkbox bool) string
}

// ActionList is a list of actions and the controls that change it: what is in
// it, where an add goes, where a remove goes, and how to build the parameter
// form for one of them.
//
// A struct for the same reason ParamForm is one. A state's entry list, a
// state's exit list and a transition's actions are the same list with three
// owners; the alternative is three templates that agree until one of them
// grows a feature.
type ActionList struct {
	Title  string // "entry actions", "exit actions", "actions"
	Scope  string // what keeps the three sets of test ids apart
	Specs  []ActionSpec
	Add    string
	Remove func(index int) string
	Form   func(index int, spec ActionSpec) ParamForm
	// Note asks for the line saying the catalogue is closed. Once per panel,
	// not once per list.
	Note bool
}

// stateActionList is one of the two lists a state carries, in authored order —
// which is the order they run in, so sorting them would be sorting the logic.
func stateActionList(data Data, kind string) ActionList {
	return ActionList{
		Title:  kind + " actions",
		Scope:  kind,
		Specs:  stateActions(data, kind),
		Add:    addActionAction(data, kind),
		Remove: func(i int) string { return removeActionAction(data, kind, i) },
		Form:   func(i int, spec ActionSpec) ParamForm { return stateActionForm(data, kind, i, spec) },
		Note:   kind == "entry",
	}
}

// stateActionForm is the form for one action in a state's entry or exit list.
func stateActionForm(data Data, kind string, index int, spec agent.ActionSpec) ParamForm {
	return ParamForm{
		Scope:  kind + "-" + strconv.Itoa(index),
		What:   "action",
		Params: actionParams(data, spec.Type),
		Values: spec.Params,
		Post: func(param string, checkbox bool) string {
			return paramAction(data, kind, index, param, checkbox)
		},
	}
}

// paramValue is what the file holds for one parameter, as a form field shows it.
//
// The value only. A registered default is a placeholder, never a value: writing
// it in would put the default into the file as though someone had chosen it,
// and there would then be no way to say "leave this alone".
func paramValue(values map[string]any, name string) string {
	v, ok := values[name]
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

func paramIsChecked(values map[string]any, name string) bool {
	v, ok := values[name]
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
func paramProblems(values map[string]any, p ParamSchema, what string) []components.Problem {
	if !p.Required || paramValue(values, p.Name) != "" {
		return nil
	}
	return []components.Problem{{
		Field:   p.Name,
		Message: p.Name + " is required — the machine still loads, and the " + what + " fails when it runs.",
	}}
}

func paramProblemsID(scope, param string) string {
	return components.ProblemsID("param", scope, param)
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
