package main

import (
	"testing"
	"time"
)

// events turns a key plan into timestamped events, optionally warping durations.
func events(plan []keyStep, warp func(keyStep) time.Duration) []keyEvent {
	var ev []keyEvent
	t := time.Unix(0, 0)
	for _, s := range plan {
		ev = append(ev, keyEvent{down: s.down, at: t})
		t = t.Add(warp(s))
	}
	return ev
}

func TestKeyPlanTiming(t *testing.T) {
	plan := keyPlan("SOS", 15) // unit = 80ms
	var down, up time.Duration
	for _, s := range plan {
		if s.down {
			down += s.dur
		} else {
			up += s.dur
		}
	}
	// S = 3 dots (3u), O = 3 dashes (9u), S = 3u: 15u key down = 1200ms.
	// Gaps: 2+2+2 symbol gaps (6u) + 2 letter gaps (6u) = 12u = 960ms.
	if down != 1200*time.Millisecond || up != 960*time.Millisecond {
		t.Fatalf("down=%v up=%v, want 1.2s / 960ms", down, up)
	}
	if last := plan[len(plan)-1]; last.down {
		t.Fatal("plan must end with the key up")
	}
}

func TestKeyPlanWordGap(t *testing.T) {
	plan := keyPlan("e e", 15)
	// dot, 7u gap, dot, trailing up
	if len(plan) != 4 || plan[1].down || plan[1].dur != 7*80*time.Millisecond {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	plan := keyPlan("HELLO WORLD 73", 20)
	text, raw, _ := decodeMorse(events(plan, func(s keyStep) time.Duration { return s.dur }))
	if text != "HELLO WORLD 73" {
		t.Fatalf("decoded %q (raw %q)", text, raw)
	}
}

func TestDecodeSloppyFist(t *testing.T) {
	// Short dashes, long dots and uneven gaps, like a human on a phone.
	plan := keyPlan("SOS", 12)
	i := 0
	warp := func(s keyStep) time.Duration {
		i++
		f := []float64{1.3, 0.8, 1.1, 0.9, 1.4}[i%5]
		return time.Duration(float64(s.dur) * f)
	}
	text, raw, unit := decodeMorse(events(plan, warp))
	if text != "SOS" {
		t.Fatalf("decoded %q raw %q unit %v", text, raw, unit)
	}
}

func TestDecodeUnknownAndEmpty(t *testing.T) {
	if text, _, _ := decodeMorse(nil); text != "" {
		t.Fatalf("empty input decoded to %q", text)
	}
	u := 60 * time.Millisecond
	// "........" is not a letter: must come back as ? with the raw dots intact.
	var plan []keyStep
	for i := 0; i < 8; i++ {
		if i > 0 {
			plan = append(plan, keyStep{false, u})
		}
		plan = append(plan, keyStep{true, u})
	}
	plan = append(plan, keyStep{false, 0})
	text, raw, _ := decodeMorse(events(plan, func(s keyStep) time.Duration { return s.dur }))
	if text != "?" || raw != "........" {
		t.Fatalf("got text %q raw %q", text, raw)
	}
}
