package animations

import (
	"strings"
	"testing"
)

const sampleTOML = `# keep this file's author notes
title = "an unknown top-level field"

[[animation]]
name = "idle"
sheet = "sprites/player.png" # artist choice
frames = [0] # frame selection
fps = 8 # playback note
loop = true
artist_hint = "preserve me"

[[entity_asset]]
entity_type = "Player"
sheet = "sprites/player.png"

[[animation]]
name = "walk"
sheet = "sprites/player.png"
frames = [1, 2]
fps = 12
loop = true
`

func TestParse_RoundTripsUnknownTOMLWithoutChanges(t *testing.T) {
	doc, err := Parse([]byte(sampleTOML))
	if err != nil || string(doc.Bytes()) != sampleTOML {
		t.Fatalf("lossless animation parse = %+v, %v", doc, err)
	}
	if len(doc.Animations()) != 2 || doc.Animations()[0].Name != "idle" || len(doc.Bindings()) != 1 {
		t.Fatalf("animation reading model = %+v, %+v", doc.Animations(), doc.Bindings())
	}
}

func TestSetAnimationField_PreservesCommentsUnknownKeysAndUnrelatedTables(t *testing.T) {
	for _, tt := range []struct {
		name, before, after string
		edit                func(*Document) error
	}{
		{"fps", "fps = 8 # playback note", "fps = 24 # playback note", func(d *Document) error { return d.SetFPS("idle", 24) }},
		{"frames", "frames = [0] # frame selection", "frames = [2, 4] # frame selection", func(d *Document) error { return d.SetFrames("idle", []int{2, 4}) }},
		{"loop", "loop = true\nartist_hint", "loop = false\nartist_hint", func(d *Document) error { return d.SetLoop("idle", false) }},
		{"sheet", `sheet = "sprites/player.png" # artist choice`, `sheet = "sprites/new.png" # artist choice`, func(d *Document) error { return d.SetSheet("idle", "sprites/new.png") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(sampleTOML))
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.edit(doc); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(sampleTOML, tt.before, tt.after, 1)
			if got := string(doc.Bytes()); got != want {
				t.Errorf("field edit rewrote other TOML:\n%s", got)
			}
			if _, err := Parse(doc.Bytes()); err != nil {
				t.Errorf("edited TOML no longer parses: %v", err)
			}
		})
	}
}

func TestSetFrames_ReplacesMultilineArrayWithoutEditingTheNextField(t *testing.T) {
	src := strings.Replace(sampleTOML, "frames = [0] # frame selection", "frames = [\n  0, # original frame\n  1,\n] # frame selection", 1)
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFrames("idle", []int{2, 3}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "frames = [\n  0, # original frame\n  1,\n] # frame selection", "frames = [2, 3] # frame selection", 1)
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("multiline frame edit changed unrelated bytes:\n%s", got)
	}
}

func TestSetBindingSheet_PreservesUnrelatedAnimationFields(t *testing.T) {
	doc, err := Parse([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetBindingSheet("Player", "sprites/alternate.png"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sampleTOML, `[[entity_asset]]
entity_type = "Player"
sheet = "sprites/player.png"`, `[[entity_asset]]
entity_type = "Player"
sheet = "sprites/alternate.png"`, 1)
	if got := string(doc.Bytes()); got != want {
		t.Errorf("binding edit touched unrelated TOML:\n%s", got)
	}
}

func TestParse_RejectsDuplicateAnimationNamesWithoutChoosingAWinner(t *testing.T) {
	duplicate := sampleTOML + `
[[animation]]
name = "idle"
sheet = "sprites/other.png"
frames = [0]
fps = 1
loop = true
`
	if _, err := Parse([]byte(duplicate)); err == nil || !strings.Contains(err.Error(), "duplicate animation") {
		t.Errorf("duplicate animation silently resolved to one winner: %v", err)
	}
}

func TestSetFPS_IgnoresFakeTableInsideAnUnknownMultilineString(t *testing.T) {
	src := `description = """a note
[[animation]]
name = "fake"
fps = 91
"""
` + sampleTOML
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("idle", 4); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.Bytes()), "fps = 91\n") || !strings.Contains(string(doc.Bytes()), "fps = 4 # playback note") {
		t.Errorf("multiline string confused section scanner:\n%s", doc.Bytes())
	}
}

func TestSetFPS_IgnoresHeaderShapedLinesInsideUnknownArray(t *testing.T) {
	src := strings.Replace(sampleTOML, "fps = 8 # playback note", "palette = [\n  [1, 2],\n  [3, 4],\n]\nfps = 8 # playback note", 1)
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("idle", 24); err != nil {
		t.Fatal(err)
	}
	if got, want := string(doc.Bytes()), strings.Replace(src, "fps = 8 # playback note", "fps = 24 # playback note", 1); got != want {
		t.Fatalf("nested array ended the animation section:\n%s", got)
	}
}

func TestSetField_ReplacesEntireMultilineTOMLString(t *testing.T) {
	for _, tt := range []struct {
		name, before, after string
		edit                func(*Document) error
	}{
		{"sheet", "sheet = '''sprites/\nplayer.png''' # artist choice", `sheet = "sprites/new.png" # artist choice`, func(d *Document) error { return d.SetSheet("idle", "sprites/new.png") }},
		{"name", "name = \"\"\"id\nle\"\"\"", `name = "stand"`, func(d *Document) error { return d.RenameAnimation("id\nle", "stand") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := sampleTOML
			if tt.name == "sheet" {
				src = strings.Replace(src, `sheet = "sprites/player.png" # artist choice`, tt.before, 1)
			} else {
				src = strings.Replace(src, `name = "idle"`, tt.before, 1)
			}
			doc, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.edit(doc); err != nil {
				t.Fatal(err)
			}
			if got, want := string(doc.Bytes()), strings.Replace(src, tt.before, tt.after, 1); got != want {
				t.Fatalf("multiline value edit changed unrelated bytes:\n%s", got)
			}
		})
	}
}

func TestSetSheet_ReplacesMultilineStringEndingInOneOrTwoQuotes(t *testing.T) {
	for _, tt := range []struct {
		name, value string
	}{
		{"one quote before delimiter", `sheet = """sprites/player.png""""`},
		{"two quotes before delimiter", `sheet = """sprites/player.png"""""`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(sampleTOML, `sheet = "sprites/player.png" # artist choice`, tt.value+" # artist choice", 1)
			doc, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if err := doc.SetSheet("idle", "sprites/new.png"); err != nil {
				t.Fatal(err)
			}
			if got, want := string(doc.Bytes()), strings.Replace(src, tt.value, `sheet = "sprites/new.png"`, 1); got != want {
				t.Fatalf("quoted closing delimiter was left behind:\n%s", got)
			}
		})
	}
}

func TestSetFPS_IgnoresFakeFieldInsideTheSelectedSectionsMultilineString(t *testing.T) {
	src := `[[animation]]
name = "idle"
sheet = "sprites/player.png"
frames = [0]
artist_note = """
fps = 99
"""
fps = 8
loop = true
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("idle", 4); err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Bytes()); got != strings.Replace(src, "fps = 8\n", "fps = 4\n", 1) {
		t.Errorf("field-like prose was edited instead of the real TOML value:\n%s", got)
	}
}

func TestSetFPS_QuotedAndPlainTableHeadersKeepTheirOwnNames(t *testing.T) {
	src := `[["animation"]]
name = "first"
sheet = "sprites/a.png"
frames = [0]
fps = 1
loop = true

[[animation]]
name = "second"
sheet = "sprites/b.png"
frames = [0]
fps = 2
loop = true
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("first", 24); err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Bytes()); !strings.Contains(got, "fps = 24\n") || !strings.Contains(got, "fps = 2\n") {
		t.Fatalf("mixed table headers retargeted a different animation:\n%s", got)
	}
}

func TestSetFPS_LiteralQuotedTableHeaderStillResolvesItsOwnAnimation(t *testing.T) {
	src := `[['animation']]
name = "first"
sheet = "sprites/a.png"
frames = [0]
fps = 1
loop = true
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("first", 6); err != nil || !strings.Contains(string(doc.Bytes()), "fps = 6") {
		t.Fatalf("literal-quoted header edit = %v, %s", err, doc.Bytes())
	}
}

func TestSetFPS_QuotedKeysPreserveTheirSpellingAndComments(t *testing.T) {
	for _, tt := range []struct {
		name, key string
	}{{"basic quoted", `"fps"`}, {"literal quoted", `'fps'`}} {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(sampleTOML, "fps = 8 # playback note", tt.key+" = 8 # playback note", 1)
			doc, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if err := doc.SetFPS("idle", 24); err != nil {
				t.Fatal(err)
			}
			if got, want := string(doc.Bytes()), strings.Replace(src, tt.key+" = 8 # playback note", tt.key+" = 24 # playback note", 1); got != want {
				t.Errorf("quoted field edit rewrote TOML:\n%s", got)
			}
		})
	}
}

func TestRenameAnimation_ChangesOnlyItsOwnNameAndRefusesCollision(t *testing.T) {
	doc, err := Parse([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.RenameAnimation("idle", "stand"); err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Bytes()); got != strings.Replace(sampleTOML, `name = "idle"`, `name = "stand"`, 1) {
		t.Errorf("rename rewrote unrelated tables:\n%s", got)
	}
	before := string(doc.Bytes())
	if err := doc.RenameAnimation("stand", "walk"); err == nil || !strings.Contains(err.Error(), "duplicate") || string(doc.Bytes()) != before {
		t.Errorf("colliding animation rename = %v, file %s", err, doc.Bytes())
	}
}

func TestRenameBinding_ChangesOnlyItsOwnEntityType(t *testing.T) {
	doc, err := Parse([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.RenameBinding("Player", "Hero"); err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Bytes()); got != strings.Replace(sampleTOML, `entity_type = "Player"`, `entity_type = "Hero"`, 1) {
		t.Errorf("binding rename rewrote another table:\n%s", got)
	}
}

func TestAddAnimationAndBinding_PreserveExistingFile(t *testing.T) {
	doc, err := Parse([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.AddAnimation("jump", "sprites/new.png", []int{0, 2}, 8, false); err != nil {
		t.Fatal(err)
	}
	if err := doc.AddBinding("Goblin", "sprites/goblin.png"); err != nil {
		t.Fatal(err)
	}
	got := string(doc.Bytes())
	if !strings.HasPrefix(got, sampleTOML) || !strings.Contains(got, `name = "jump"`) || !strings.Contains(got, `entity_type = "Goblin"`) {
		t.Fatalf("appending new entries rewrote authored TOML:\n%s", got)
	}
	if _, err := Parse(doc.Bytes()); err != nil {
		t.Fatalf("appended TOML cannot load: %v", err)
	}
	before := string(doc.Bytes())
	if err := doc.AddAnimation("jump", "sprites/other.png", []int{0}, 4, true); err == nil || string(doc.Bytes()) != before {
		t.Error("duplicate append damaged working file")
	}
}

func TestSetAnimationField_RefusesUnplayableValuesWithoutChangingBytes(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Document) error
	}{
		{"zero fps", func(d *Document) error { return d.SetFPS("idle", 0) }},
		{"negative fps", func(d *Document) error { return d.SetFPS("idle", -3) }},
		{"no frames", func(d *Document) error { return d.SetFrames("idle", nil) }},
		{"negative frame", func(d *Document) error { return d.SetFrames("idle", []int{-1}) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(sampleTOML))
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.edit(doc); err == nil || string(doc.Bytes()) != sampleTOML {
				t.Errorf("unsafe edit changed animation TOML: %v, %s", err, doc.Bytes())
			}
		})
	}
}

func TestSetFPS_AddsAnAbsentOptionalFieldInsideTheRightAnimation(t *testing.T) {
	src := strings.Replace(sampleTOML, "fps = 8 # playback note\n", "", 1)
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFPS("idle", 7); err != nil {
		t.Fatal(err)
	}
	text := string(doc.Bytes())
	first := strings.Split(text, "[[entity_asset]]")[0]
	if !strings.Contains(first, "fps = 7") || !strings.Contains(text, "fps = 12") || !strings.Contains(text, "artist_hint = \"preserve me\"") {
		t.Errorf("new animation field landed in the wrong table or lost other fields:\n%s", text)
	}
}
