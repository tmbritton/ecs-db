package builtins

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/tilemap"
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

type moveTowardTargetAction struct{ grid *tilemap.TileGrid }

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
	if a.grid != nil {
		space, mover, err := currentMover(a.grid, ctx.Reader, ctx.EntityID)
		if err != nil {
			return fmt.Errorf("moveTowardTarget: %w", err)
		}
		// Speed can exceed one cell. Check every crossed grid cell, not only the
		// landing cell, so a fast entity cannot jump an obstructing wall.
		samples := int(math.Ceil(math.Max(math.Abs(newX-toFloat(px)), math.Abs(newY-toFloat(py))) * 2))
		if samples < 1 {
			samples = 1
		}
		for i := 1; i <= samples; i++ {
			at := tilemap.Point{
				X: int(math.Floor(toFloat(px) + (newX-toFloat(px))*float64(i)/float64(samples))),
				Y: int(math.Floor(toFloat(py) + (newY-toFloat(py))*float64(i)/float64(samples))),
			}
			allowed, err := space.CanEnter(mover, at)
			if err != nil {
				return fmt.Errorf("moveTowardTarget: %w", err)
			}
			if !allowed {
				return nil
			}
		}
	}

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

// ── setAnimation ──────────────────────────────────────────────────────────────

type setAnimationAction struct{}

func (a *setAnimationAction) Run(ctx agent.ActionContext) error {
	anim, _ := ctx.Params["animation"].(string)
	return ctx.World.SetComponentValue(ctx.EntityID, "Sprite", "animation", anim)
}

// ── computePath ───────────────────────────────────────────────────────────────

type computePathAction struct{ grid *tilemap.TileGrid }

func currentMover(grid *tilemap.TileGrid, reader agent.WorldReader, entityID int64) (tilemap.Space, tilemap.SpatialEntity, error) {
	query, ok := reader.(tilemap.SpaceQuerier)
	if !ok {
		return tilemap.Space{}, tilemap.SpatialEntity{}, fmt.Errorf("world reader does not expose the current spatial transaction")
	}
	space, err := grid.SpaceFromSnapshot(query)
	if err != nil {
		return tilemap.Space{}, tilemap.SpatialEntity{}, err
	}
	mover, found := space.Entity(entityID)
	if !found {
		return tilemap.Space{}, tilemap.SpatialEntity{}, fmt.Errorf("entity %d has no Position", entityID)
	}
	return space, mover, nil
}

func (a *computePathAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}

	px, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "y")

	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	if txComp == "" || tyComp == "" {
		return nil
	}
	tx, _ := ctx.Reader.GetComponentValue(ctx.EntityID, txComp, "target_x")
	ty, _ := ctx.Reader.GetComponentValue(ctx.EntityID, tyComp, "target_y")

	start := tilemap.Point{X: int(toFloat(px)), Y: int(toFloat(py))}
	goal := tilemap.Point{X: int(toFloat(tx)), Y: int(toFloat(ty))}

	space, mover, err := currentMover(a.grid, ctx.Reader, ctx.EntityID)
	if err != nil {
		return fmt.Errorf("computePath: %w", err)
	}
	path, err := tilemap.AStarFor(space, mover, start, goal)
	if err != nil {
		return fmt.Errorf("computePath: %w", err)
	}
	if path == nil {
		return nil
	}

	jsonBytes, err := json.Marshal(path)
	if err != nil {
		return nil
	}
	jsonStr := string(jsonBytes)

	has, _ := ctx.Reader.HasComponent(ctx.EntityID, "Path")
	if !has {
		return ctx.World.AttachComponent(ctx.EntityID, "Path", map[string]any{
			"waypoints":     jsonStr,
			"current_index": int64(0),
		})
	}
	if err := ctx.World.SetComponentValue(ctx.EntityID, "Path", "waypoints", jsonStr); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, "Path", "current_index", int64(0))
}

// ── stepAlongPath ─────────────────────────────────────────────────────────────

type stepAlongPathAction struct{ grid *tilemap.TileGrid }

func (a *stepAlongPathAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}

	waypointsVal, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Path", "waypoints")
	indexVal, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Path", "current_index")

	waypointsStr, _ := waypointsVal.(string)
	if waypointsStr == "" {
		return nil
	}

	var waypoints []tilemap.Point
	if err := json.Unmarshal([]byte(waypointsStr), &waypoints); err != nil {
		return nil
	}

	idx := int(toFloat(indexVal))
	if idx >= len(waypoints) {
		return nil
	}

	next := waypoints[idx]
	space, mover, err := currentMover(a.grid, ctx.Reader, ctx.EntityID)
	if err != nil {
		return fmt.Errorf("stepAlongPath: %w", err)
	}
	allowed, err := space.CanEnter(mover, next)
	if err != nil {
		return fmt.Errorf("stepAlongPath: %w", err)
	}
	if !allowed {
		return nil
	}
	if err := ctx.World.SetComponentValue(ctx.EntityID, "Position", "x", float64(next.X)); err != nil {
		return err
	}
	if err := ctx.World.SetComponentValue(ctx.EntityID, "Position", "y", float64(next.Y)); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, "Path", "current_index", int64(idx+1))
}
