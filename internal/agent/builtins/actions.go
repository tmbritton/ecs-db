package builtins

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// toFloat coerces any SQLite-returned or JSON-decoded numeric value to float64.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// resolveTargetID resolves a "$player" sentinel or numeric entity ID param.
func resolveTargetID(reader agent.WorldReader, targetParam any) (int64, bool) {
	switch v := targetParam.(type) {
	case string:
		if v == "$player" {
			id, err := reader.FindEntityByType("Player")
			if err != nil {
				return 0, false
			}
			return id, true
		}
		fmt.Printf("[agent] resolveTargetID: unknown sentinel %q\n", v)
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

// manifestComp returns the component name for a context key, or "" if absent.
func manifestComp(ctx agent.ActionContext, key string) string {
	if ctx.ContextManifest == nil {
		return ""
	}
	return ctx.ContextManifest[key]
}

// ── setTimer ──────────────────────────────────────────────────────────────────

type setTimerAction struct{}

func (a *setTimerAction) Run(ctx agent.ActionContext) error {
	key, _ := ctx.Params["key"].(string)
	if key == "" {
		return nil
	}
	comp := manifestComp(ctx, key)
	if comp == "" {
		fmt.Printf("[agent] setTimer: key %q not in ContextManifest\n", key)
		return nil
	}
	ticks := int64(toFloat(ctx.Params["ticks"]))
	return ctx.World.SetComponentValue(ctx.EntityID, comp, key, ticks)
}

// ── moveTowardTarget ──────────────────────────────────────────────────────────

type moveTowardTargetAction struct{}

func (a *moveTowardTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	px, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "y")

	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	speedComp := manifestComp(ctx, "speed")
	if txComp == "" || tyComp == "" || speedComp == "" {
		return nil
	}

	tx, _ := ctx.Reader.GetComponentValue(ctx.EntityID, txComp, "target_x")
	ty, _ := ctx.Reader.GetComponentValue(ctx.EntityID, tyComp, "target_y")
	speedVal, _ := ctx.Reader.GetComponentValue(ctx.EntityID, speedComp, "speed")

	speed := toFloat(speedVal)
	if mult, ok := ctx.Params["speed_mult"]; ok {
		speed *= toFloat(mult)
	}

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 0.001 {
		return nil
	}
	step := math.Min(speed, dist)
	newX := toFloat(px) + (dx/dist)*step
	newY := toFloat(py) + (dy/dist)*step

	if err := ctx.World.SetComponentValue(ctx.EntityID, "Position", "x", newX); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, "Position", "y", newY)
}

// ── pickRandomTarget ──────────────────────────────────────────────────────────

type pickRandomTargetAction struct{}

func (a *pickRandomTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	if txComp == "" || tyComp == "" {
		return nil
	}

	px, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "y")
	radius := toFloat(ctx.Params["radius"])

	angle := rand.Float64() * 2 * math.Pi
	dist := math.Sqrt(rand.Float64()) * radius
	newTX := toFloat(px) + dist*math.Cos(angle)
	newTY := toFloat(py) + dist*math.Sin(angle)

	if err := ctx.World.SetComponentValue(ctx.EntityID, txComp, "target_x", newTX); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, tyComp, "target_y", newTY)
}

// ── setPursueTarget ───────────────────────────────────────────────────────────

type setPursueTargetAction struct{}

func (a *setPursueTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	if txComp == "" || tyComp == "" {
		return nil
	}

	playerID, ok := resolveTargetID(ctx.Reader, "$player")
	if !ok {
		return nil
	}
	playerX, _ := ctx.Reader.GetComponentValue(playerID, "Position", "x")
	playerY, _ := ctx.Reader.GetComponentValue(playerID, "Position", "y")

	if err := ctx.World.SetComponentValue(ctx.EntityID, txComp, "target_x", toFloat(playerX)); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, tyComp, "target_y", toFloat(playerY))
}

// ── dealDamage ────────────────────────────────────────────────────────────────

type dealDamageAction struct{}

func (a *dealDamageAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	amount := toFloat(ctx.Params["amount"])
	targetID, ok := resolveTargetID(ctx.Reader, ctx.Params["target"])
	if !ok {
		return nil
	}
	hp, err := ctx.Reader.GetComponentValue(targetID, "Health", "hp")
	if err != nil {
		return fmt.Errorf("dealDamage: read hp: %w", err)
	}
	return ctx.World.SetComponentValue(targetID, "Health", "hp", toFloat(hp)-amount)
}

// ── spawnEntity ───────────────────────────────────────────────────────────────

type spawnEntityAction struct{}

func (a *spawnEntityAction) Run(ctx agent.ActionContext) error {
	entityType, _ := ctx.Params["entity_type"].(string)
	if entityType == "" {
		return nil
	}
	id, err := ctx.World.SpawnEntity(entityType)
	if err != nil {
		return err
	}
	fmt.Printf("[agent] spawnEntity: created entity %d of type %q\n", id, entityType)
	return nil
}

// ── attachComponent ───────────────────────────────────────────────────────────

type attachComponentAction struct{}

func (a *attachComponentAction) Run(ctx agent.ActionContext) error {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return nil
	}
	var data map[string]any
	if d, ok := ctx.Params["data"]; ok {
		data, _ = d.(map[string]any)
	}
	if data == nil {
		data = map[string]any{}
	}
	return ctx.World.AttachComponent(ctx.EntityID, compName, data)
}

// ── detachComponent ───────────────────────────────────────────────────────────

type detachComponentAction struct{}

func (a *detachComponentAction) Run(ctx agent.ActionContext) error {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return nil
	}
	return ctx.World.DetachComponent(ctx.EntityID, compName)
}

// ── log ───────────────────────────────────────────────────────────────────────

type logAction struct{}

func (a *logAction) Run(ctx agent.ActionContext) error {
	msg := fmt.Sprintf("%v", ctx.Params["message"])
	fmt.Printf("[agent log] %s\n", msg)
	return nil
}
