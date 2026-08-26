# Story 8: A spawn is re-imported like a tile

**Epic:** 14 — Tiled map & tileset formats
**Status:** ✅ Complete
**Priority:** High — supersedes Story 6's central decision, which was wrong

**Depends on:** Story 6, whose rule this replaces

## Context

Story 6 decided that a spawn is created once and then never updated or deleted,
and argued it this way:

> An entity created from a spawn stops being the file's the moment the game
> runs. The file says where a world *starts*, not what it currently is.

Story 3 had already decided the opposite for tiles, and written down why:

> **The file wins for what the file describes.** A tile's passability and its
> type come from the map on every load, overwriting whatever the running game
> left there — setTilePassable opening a door is state from one session, and the
> file is the level. The alternative, letting the database win, would mean an
> author who walls off a corridor in Forge saves, runs, and finds the corridor
> still open: the exact failure re-import exists to end, moved one field to the
> left.

Two rules, one file, no reason for the difference. Story 6 named the divergence
and defended it; the defence is the same sentence Story 3 rejected, with "goblin"
in place of "corridor".

### Why the divergence looked reasonable, and was not

The engine was written before there was an editor, when a map file was authored
once and the database was the world from then on. Under that assumption "the
entity is the game's now" is nearly true, because nothing was going to edit the
file again.

There is an editor now, and it does not change the *hard* part in the way it
first appears to. Forge writes files and opens the database **read-only** —
three opens, all through `storage.OpenReadOnly`/`ReadOnlyDSN`, zero write
statements in `internal/forge`, and `query_only(true)` in the DSN so the
connection refuses a write rather than the code remembering not to. So there is
no two-writer drift to reconcile and never was.

What the editor changes is how often the file is edited *after* the world exists,
which is what turns "create once" from a shortcut into a lie: drag a goblin,
save, run, and it is where it was; delete it, and it is still there; fix its
`maxHp`, and nothing happens.

## The rule, which is Story 3's

**The file wins for what the file describes.** An object's class, its position
and every `Component.property` it declares are re-applied on every load.

**The entity id survives.** It is the one thing that cannot be recreated —
`behavior_components` and `transitions` name it, and a machine's running state
hangs off it — so a changed spawn is an update, never a delete and an insert.

**What the file says nothing about, this says nothing about.** An entity's `hp`
after a fight, a component attached at runtime, a machine mid-flight: all
untouched, because the update is partial. `SetComponentValues` is already exactly
this and says so.

**An object removed from the map deletes the entity it made.** That is what the
tile rule does with a cell the file stopped describing, and it is the half Story
6 got most wrong: an author deleting a goblin in Forge and finding it still there
has no other gesture available.

### What it costs, stated rather than discovered

A goblin that walked across the room returns to its spawn point when the map is
re-imported, exactly as a door opened by `setTilePassable` closes again. Both are
"state from one session" against "the file is the level". Re-import happens when
the map is loaded, so this is a level load rather than a per-tick correction.

An entity killed at runtime comes back the next time the map loads, for the same
reason. Persisting deaths across a reload is a save-game, and this engine has no
concept of one yet; when it does, it will need to say which of the two it is.

## Acceptance Criteria

- [x] An object whose properties changed updates its entity in place, keeping the
      entity id
- [x] An object that moved moves its entity
- [x] An object removed from the map deletes the entity it made, and its spawn row
- [x] Components the object does not mention are left alone, values and all
- [x] A component the object newly mentions is attached to the existing entity
- [x] An entity deleted at runtime is recreated while its object is still there
- [x] Entities nobody spawned are never touched
- [x] The result says what it did, on the same terms as `SyncTiles`' Result
- [x] `go test ./...` passes

## Design

`SyncSpawns` becomes the shape `SyncTiles` already has: read what is there, plan
against what the file says, apply.

```go
type SpawnResult struct {
    Created, Updated, Deleted, Unchanged int
    Refused, Warnings                    []string
    Skipped                              int
}
```

The `spawns` table stays, and its foreign key changes from `ON DELETE SET NULL`
to `ON DELETE CASCADE`. Under the old rule the row had to outlive its entity, to
record that an import had happened and stop a dead spawn returning. Under this
one a dead spawn *does* return, so the row is a live link and nothing else: when
the entity goes the link goes, and the file puts both back.

An entity is only ever deleted through a spawn row, so nothing created by hand
or by a script is in scope.

## As Implemented

The first attempt claimed this rule and did not implement it. The review found
six ways, and the through-line is that copying `SyncTiles`' *slogan* is not the
same as copying what it does.

**Every existing database was broken, permanently.** `EnsureInterpreterTables`
is `CREATE TABLE IF NOT EXISTS`, so changing the foreign key in the DDL changes
nothing about a database that already exists. A row left on `SET NULL` whose
entity died holds a NULL, the importer reads that as "not spawned", and its
`INSERT` collides with the primary key the row still occupies — refusing the
object on every load, forever, with a SQLite constraint error naming an internal
table. There is a migration now, and `RecordSpawn` is an upsert so a stale row
of any origin is replaced rather than collided with.

**The update path performed no validation at all.** Creating went through
`CreateEntityInTx` and validated; updating wrote straight to the storage port.
So an object could add a component its type forbids, or stop naming a required
one, and the same file made a different world depending on whether it had been
loaded before — which is the exact failure re-import exists to end, one level up
from the one this story is about. Both paths run the same check now.

**"`Tx` has no reader" was a rationalisation.** It was the stated reason for
re-applying every value on every load instead of diffing. `SyncTiles` has no
`Tx` reader either — it reads the pool before the transaction opens, and says so
at length — and `SyncSpawns` is handed the same `*sql.DB` and already reads it
three lines earlier. Measured, an unedited two-goblin map issued six UPDATEs per
load. It diffs on the pool now, and a test with triggers asserts that loading an
unedited map writes nothing at all, which is `SyncTiles`' actual promise.

**The class was not re-applied**, though this story's own rule said it was.
Writing the new class's components onto an entity still typed as the old one
produces a row that breaks its own contract. A class change is now delete and
create — the entity id changes, because it is not the same thing any more.

**A scalar component could never be given a value from a map.** It has no
`Properties` to look a name up in, so `Label.value` was refused outright; and
what did get through was keyed by property name, which the insert path tolerates
and `SetComponentValues` does not, so anything that survived was refused on the
next load. A non-object component is addressed by its column now.

**Deleting was one transaction per object**, so a failure part way through left
a half-deleted world *and* aborted the load. It is one transaction, which is
`SyncTiles`' reason exactly. The comment claiming "nothing can force that failure
in a test" was wrong — the reviewer forced it with a trigger.

And the guard that stands between a typo and data loss — marking an object seen
*before* its properties are parsed, so a refused object is not mistaken for one
the map no longer has — had no test. My battery missed it; a mutation moving one
line survived the whole suite.

## Notes

- **Clearing an object's class deletes its entity.** Correct under the rule, and
  a great deal of consequence for emptying a field in the editor, so it is
  warned about rather than done quietly.
- **The map path is still the key**, and re-spelling or renaming it still reads
  as a different map. This story makes the consequence worse: the old rows are
  not only left behind, they are now unreachable, because deletion is scoped to
  the map being loaded. The author's only gesture — deleting the object — cannot
  reach them. That is the next thing to fix, and Forge's MAP mode makes renaming
  a level a menu item.
- **Deleting a spawned entity cascades into other entities' entity-ref
  components.** `DeleteEntity` documents that as a hazard; this story makes it
  reachable from an ordinary editor gesture for the first time.
