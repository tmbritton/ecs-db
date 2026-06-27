package renderer

// AnimState tracks animation playback for a single entity.
type AnimState struct {
	CurrentAnim string
	Frame       int
	Elapsed     float64 // seconds since last frame advance
}

// Advance increments the frame timer by dt seconds and steps the frame when the interval elapses.
func (s *AnimState) Advance(def *AnimDef, dt float64) {
	if def.FPS <= 0 || len(def.Frames) == 0 {
		return
	}
	s.Elapsed += dt
	if s.Elapsed >= 1.0/def.FPS {
		s.Elapsed = 0
		s.Frame++
		if def.Loop {
			s.Frame %= len(def.Frames)
		} else if s.Frame >= len(def.Frames) {
			s.Frame = len(def.Frames) - 1
		}
	}
}
