package builtins

import "github.com/tmbritton/ecs-db/internal/agent"

// NewRegistry returns a registry pre-populated with all built-in actions and guards.
func NewRegistry() *agent.Registry {
	r := agent.NewRegistry()
	RegisterBuiltins(r)
	return r
}

// RegisterBuiltins registers all built-in actions and guards into r.
func RegisterBuiltins(r *agent.Registry) {
	registerActions(r)
	registerGuards(r)
}

func registerActions(r *agent.Registry) {
	r.RegisterAction(agent.ActionMeta{
		Name:        "setTimer",
		Description: "Write a tick count to a named timer field in the entity's context component.",
		Params: []agent.ParamSchema{
			{Name: "key", Type: "string", Required: true},
			{Name: "ticks", Type: "number", Required: true},
		},
	}, &setTimerAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "moveTowardTarget",
		Description: "Move entity one step toward target_x/target_y at speed (× speed_mult if provided).",
		Params: []agent.ParamSchema{
			{Name: "speed_mult", Type: "number", Required: false, Default: 1.0},
		},
	}, &moveTowardTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "pickRandomTarget",
		Description: "Pick a random position within radius of entity's position and write to target_x/target_y.",
		Params: []agent.ParamSchema{
			{Name: "radius", Type: "number", Required: true},
		},
	}, &pickRandomTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "setPursueTarget",
		Description: "Copy the Player entity's position into entity's target_x/target_y.",
	}, &setPursueTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "dealDamage",
		Description: "Decrement Health.hp on target entity by amount. Target is an entity ID or \"$player\".",
		Params: []agent.ParamSchema{
			{Name: "amount", Type: "number", Required: true},
			{Name: "target", Type: "string", Required: false, Default: "$player"},
		},
	}, &dealDamageAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "spawnEntity",
		Description: "Create a new entity of the given type; logs the new entity ID.",
		Params: []agent.ParamSchema{
			{Name: "entity_type", Type: "string", Required: true},
		},
	}, &spawnEntityAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "attachComponent",
		Description: "Attach a component to the current entity with optional initial field values.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
			{Name: "data", Type: "object", Required: false},
		},
	}, &attachComponentAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "detachComponent",
		Description: "Detach a component from the current entity.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
		},
	}, &detachComponentAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "log",
		Description: "Print params.message to the interpreter log. No database effect.",
		Params: []agent.ParamSchema{
			{Name: "message", Type: "string", Required: true},
		},
	}, &logAction{})
}

func registerGuards(r *agent.Registry) {
	r.RegisterGuard(agent.GuardMeta{
		Name:        "timerExpired",
		Description: "True when the named timer field is ≤ 0.",
		Params: []agent.ParamSchema{
			{Name: "key", Type: "string", Required: true},
		},
	}, &timerExpiredGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "atTarget",
		Description: "True when entity's Position is within 1 unit of target_x/target_y.",
	}, &atTargetGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "inRange",
		Description: "True when distance between this entity and target is ≤ distance param.",
		Params: []agent.ParamSchema{
			{Name: "target", Type: "string", Required: true},
			{Name: "distance", Type: "number", Required: true},
		},
	}, &inRangeGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "hasComponent",
		Description: "True when the entity has the named component attached.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
		},
	}, &hasComponentGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "healthAbove",
		Description: "True when Health.hp > threshold.",
		Params: []agent.ParamSchema{
			{Name: "threshold", Type: "number", Required: true},
		},
	}, &healthAboveGuard{})
}
