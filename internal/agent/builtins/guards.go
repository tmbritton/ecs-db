package builtins

import (
	"math"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// ── timerExpired ──────────────────────────────────────────────────────────────

type timerExpiredGuard struct{}

func (g *timerExpiredGuard) Evaluate(ctx agent.GuardContext) bool {
	key, _ := ctx.Params["key"].(string)
	if key == "" || ctx.ContextManifest == nil {
		return false
	}
	comp := ctx.ContextManifest[key]
	if comp == "" {
		return false
	}
	val, _ := ctx.World.GetComponentValue(ctx.EntityID, comp, key)
	return toFloat(val) <= 0
}

// ── atTarget ──────────────────────────────────────────────────────────────────

type atTargetGuard struct{}

func (g *atTargetGuard) Evaluate(ctx agent.GuardContext) bool {
	if ctx.ContextManifest == nil {
		return false
	}
	txComp := ctx.ContextManifest["target_x"]
	tyComp := ctx.ContextManifest["target_y"]
	if txComp == "" || tyComp == "" {
		return false
	}

	if ok, _ := ctx.World.HasComponent(ctx.EntityID, "Position"); !ok {
		return false
	}

	px, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "y")
	tx, _ := ctx.World.GetComponentValue(ctx.EntityID, txComp, "target_x")
	ty, _ := ctx.World.GetComponentValue(ctx.EntityID, tyComp, "target_y")

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	return math.Sqrt(dx*dx+dy*dy) <= 1.0
}

// ── inRange ───────────────────────────────────────────────────────────────────

type inRangeGuard struct{}

func (g *inRangeGuard) Evaluate(ctx agent.GuardContext) bool {
	maxDist := toFloat(ctx.Params["distance"])
	targetID, ok := resolveTargetID(ctx.World, ctx.Params["target"])
	if !ok {
		return false
	}

	if ok, _ := ctx.World.HasComponent(ctx.EntityID, "Position"); !ok {
		return false
	}
	if ok, _ := ctx.World.HasComponent(targetID, "Position"); !ok {
		return false
	}

	px, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "y")
	tx, _ := ctx.World.GetComponentValue(targetID, "Position", "x")
	ty, _ := ctx.World.GetComponentValue(targetID, "Position", "y")

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	return math.Sqrt(dx*dx+dy*dy) <= maxDist
}

// ── hasComponent ──────────────────────────────────────────────────────────────

type hasComponentGuard struct{}

func (g *hasComponentGuard) Evaluate(ctx agent.GuardContext) bool {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return false
	}
	has, _ := ctx.World.HasComponent(ctx.EntityID, compName)
	return has
}

// ── healthAbove ───────────────────────────────────────────────────────────────

type healthAboveGuard struct{}

func (g *healthAboveGuard) Evaluate(ctx agent.GuardContext) bool {
	threshold := toFloat(ctx.Params["threshold"])
	hp, _ := ctx.World.GetComponentValue(ctx.EntityID, "Health", "hp")
	return toFloat(hp) > threshold
}
