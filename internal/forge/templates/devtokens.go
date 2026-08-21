package templates

import (
	"regexp"

	"github.com/a-h/templ"
)

// swatchStyle is the one place a colour literal legitimately reaches inline
// style: the swatch has to paint the hex it is documenting.
var hexColourRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// swatchStyle is the one place a colour literal legitimately reaches an inline
// style: a swatch has to paint the hex it documents. templ.SafeCSS bypasses
// sanitisation, so the value is validated here rather than trusting that every
// caller passes a constant.
func swatchStyle(hex string) templ.SafeCSS {
	if !hexColourRE.MatchString(hex) {
		return ""
	}
	return templ.SafeCSS("background:" + hex)
}

// The palette, grouped as the design system groups it. Kept beside the template
// so /dev/tokens and the palette test describe the same set.
var (
	Surfaces = []Swatch{
		{"--void", "#1a1714", "app backdrop"},
		{"--ink", "#201d1a", "canvas · inset wells"},
		{"--inset", "#1c1916", "deep inset · generated DDL"},
		{"--panel", "#252220", "side panels"},
		{"--raised", "#282420", "bars · rail"},
		{"--canvas", "#242019", "map canvas"},
		{"--active", "#332e28", "selected row"},
		{"--hover", "#2a2622", "hover fill"},
	}
	Borders = []Swatch{
		{"--border", "#3d372f", "default 2px stroke"},
		{"--border-dashed", "#4a4337", "add · empty slots"},
		{"--border-hi", "#5a5142", "hover / focus stroke"},
	}
	TextTones = []Swatch{
		{"--text-hi", "#f2ead9", "headings · values"},
		{"--text", "#d8d2c8", "body"},
		{"--text-secondary", "#b8ae9e", "secondary"},
		{"--text-dim", "#9a8f7d", "labels · units"},
		{"--text-faint", "#6e675c", "hints · captions"},
	}
	Accents = []Swatch{
		{"--amber", "#ffb454", "authoring · action · selection · the tool itself"},
		{"--amber-hi", "#ffc678", "amber hover"},
		{"--green", "#9ece6a", "entities · valid · on / true"},
		{"--cyan", "#56c5d0", "live source · active statechart node"},
		{"--violet", "#c8a4ff", "engine-computed values · replay source"},
		{"--red", "#e06c60", "danger · collision · breakpoint hit"},
	}
)
