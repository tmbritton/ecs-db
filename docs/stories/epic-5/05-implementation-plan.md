# Story 5: Sprite Renderer + Animation System — Implementation Plan

## New Files

### `internal/renderer/anim_loader.go`

```go
type AnimDef struct {
    Name   string
    Sheet  string
    Frames []int
    FPS    float64
    Loop   bool
}

type AnimLoader struct {
    mu   sync.RWMutex
    defs map[string]*AnimDef
}

func NewAnimLoader() *AnimLoader
func (al *AnimLoader) Load(path string) error           // parse TOML, atomic map swap
func (al *AnimLoader) Get(name string) (*AnimDef, bool) // thread-safe read
func (al *AnimLoader) Watch(ctx context.Context, path string) error // fsnotify goroutine
```

`Load` uses `github.com/BurntSushi/toml` (already in go.mod). TOML top-level is `[[animation]]` array items decoded into a `[]AnimDef` wrapper struct. On success it swaps the internal map under write-lock. On parse failure it logs and retains the previous map.

`Watch` starts a goroutine using `fsnotify.NewWatcher()` (already in go.mod). It watches the single file path, debounces at 50 ms, calls `al.Load(path)` on `Write|Create|Rename` events.

---

### `internal/renderer/anim_state.go`

```go
type AnimState struct {
    CurrentAnim string
    Frame       int
    Elapsed     float64 // seconds since last frame advance
}

func (s *AnimState) Advance(def *AnimDef, dt float64)
```

`Advance` increments `Elapsed` by `dt`. When `Elapsed >= 1.0/def.FPS`: increment `Frame`; if `Loop` wrap modulo `len(def.Frames)`, else clamp to last frame. Reset `Elapsed` to 0.

---

### `mods/assets/animations.toml`

```toml
[[animation]]
name   = "player_idle"
sheet  = "mods/assets/sprites/player.png"
frames = [0]
fps    = 1
loop   = true

[[animation]]
name   = "player_walk"
sheet  = "mods/assets/sprites/player.png"
frames = [1, 2, 3, 4]
fps    = 8
loop   = true

[[animation]]
name   = "goblin_idle"
sheet  = "mods/assets/sprites/goblin.png"
frames = [0]
fps    = 1
loop   = true

[[animation]]
name   = "goblin_walk"
sheet  = "mods/assets/sprites/goblin.png"
frames = [1, 2, 3, 4]
fps    = 8
loop   = true
```

---

### `mods/assets/sprites/player.png` and `goblin.png`

Generate via `tools/gensprites/main.go` using stdlib `image/color`, `image/draw`, `image/png`. Each PNG is 160×32 px (5 frames × 32 px tile size). Frame 0: solid base colour. Frames 1–4: base colour with a 2-pixel stripe at varying x offsets to visually distinguish walk frames. Run once and commit the binaries.

---

## Modified Files

### `internal/agent/builtins/actions.go`

Add:

```go
type setAnimationAction struct{}

func (a *setAnimationAction) Run(ctx agent.ActionContext) error {
    anim, _ := ctx.Params["animation"].(string)
    return ctx.World.SetComponentValue(ctx.EntityID, "Sprite", "animation", anim)
}
```

### `internal/agent/builtins/register.go`

Register in `registerActions()`:

```go
r.RegisterAction(agent.ActionMeta{
    Name:        "setAnimation",
    Description: "Set comp_sprite.animation on the entity.",
    Params: []agent.ParamSchema{
        {Name: "animation", Type: "string", Required: true},
    },
}, &setAnimationAction{})
```

---

### `internal/renderer/game.go`

**`NewGameParams`** — add `AnimLoader *AnimLoader`; remove `PlayerID int64`.

**`Game` struct** — remove `playerID int64`, `playerImg *ebiten.Image`; add:

```go
animLoader *AnimLoader
animStates map[int64]*AnimState
imgCache   map[string]*ebiten.Image
```

**`Update()`** — after key sampling, before tick gate, advance anim states:

```go
const dt = 1.0 / 60.0
for _, st := range g.animStates {
    if def, ok := g.animLoader.Get(st.CurrentAnim); ok {
        st.Advance(def, dt)
    }
}
```

**`Draw()`** — replace player rectangle with generic entity draw:

```sql
SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x
FROM entities e
JOIN comp_position cp ON e.id = cp.entity_id
JOIN comp_sprite   cs ON e.id = cs.entity_id
```

Per row:
1. Create `AnimState` if entity not in `animStates`.
2. If `animation != animStates[id].CurrentAnim` → reset frame/elapsed, update `CurrentAnim`.
3. Look up `AnimDef` via `animLoader.Get(animation)`.
4. If sheet is `""`, image load fails, or AnimDef missing → draw solid colour rectangle (fallback).
5. Otherwise: load/cache `*ebiten.Image` from sheet path; compute sub-rect `image.Rect(frame*tileSize, 0, (frame+1)*tileSize, tileSize)`; draw at `(x*tileSize, y*tileSize)`.
6. If `flip_x != 0`: `GeoM.Scale(-1, 1)` then `GeoM.Translate(float64(tileSize), 0)` before position translate.

After loop: delete `animStates` entries not present in the query result.

**`loadImage(path string) (*ebiten.Image, error)`** — private helper. Check `imgCache`; if miss: open file, `image.Decode`, `ebiten.NewImageFromImage`, store in cache. Never evict on anim reload.

---

### `cmd/game/main.go`

After behavior loading, find assets path and wire AnimLoader:

```go
var animPath string
for _, mod := range cfg.Mods {
    if mod.Assets != "" {
        animPath = filepath.Join(mod.Assets, "animations.toml")
        break
    }
}
animLoader := renderer.NewAnimLoader()
if animPath != "" {
    if err := animLoader.Load(animPath); err != nil {
        fmt.Fprintf(os.Stderr, "Warning: loading animations: %v\n", err)
    }
    go func() { _ = animLoader.Watch(ctx, animPath) }()
}
```

Pass `AnimLoader: animLoader` in `renderer.NewGameParams`. Remove `PlayerID` from params.

---

### `game.toml`

Change `assets = "./assets/"` → `assets = "./mods/assets/"` under `[[mods]]`.

---

## Dependency Graph

```
agent/builtins     ← adds setAnimation (no new imports)
renderer/anim_loader ← BurntSushi/toml + fsnotify (both already in go.mod)
renderer/anim_state  ← stdlib only
renderer/game      ← anim_loader + anim_state
cmd/game           ← unchanged import set
```

No new dependencies.

---

## Verification

```bash
# One-time: generate placeholder sprite PNGs
go run ./tools/gensprites

# Full test suite
go test -tags ebitengine ./...

# Run the game
go run -tags ebitengine ./cmd/game
```

Expected:
- Player renders from sprite sheet frames, not a cyan rectangle
- Idle vs walk animation visually distinct
- Editing `mods/assets/animations.toml` reloads within ~50 ms (no restart)
- Missing sheet path falls back to solid colour — no crash
