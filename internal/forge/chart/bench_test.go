package chart_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
)

// wide builds a flat machine of n states, each with two transitions: one that
// resolves and one that does not. The unresolvable one is the expensive case —
// it walks the whole tree before giving up — and a machine being edited has
// them constantly, which is the point.
func wide(n int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `{"id":"wide","initial":"s0","states":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"s%d":{"on":{"NEXT":[{"target":"s%d"}],"GONE":[{"target":"nowhere%d"}]}}`,
			i, (i+1)%n, i)
	}
	b.WriteString("}}")
	return []byte(b.String())
}

// Forge validates and redraws on every render and on every tick of a 2-second
// stream, so this is per-tick cost for one open browser tab. It is here to be
// watched: the chart is O(distinct targets × states), and the two things that
// keep that tolerable — resolving each distinct target once, and not
// reallocating the authored order at every level of every search — are both
// easy to lose in a refactor that looks harmless.
func BenchmarkBuild(b *testing.B) {
	for _, n := range []int{20, 100, 400} {
		def, err := agent.ParseMachine(wide(n))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("states=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				chart.Build(def, "")
			}
		})
	}
}
