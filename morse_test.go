package main

import (
	"testing"
	"time"
)

func exactTiming(step keyStep) time.Duration { return step.dur }

// events turns a key plan into timestamped events, with durations warped by timing.
func events(plan []keyStep, timing func(keyStep) time.Duration) []keyEvent {
	var out []keyEvent
	at := time.Unix(0, 0)
	for _, step := range plan {
		out = append(out, keyEvent{down: step.down, at: at})
		at = at.Add(timing(step))
	}
	return out
}

func TestKeyPlanTiming(t *testing.T) {
	plan := keyPlan("SOS", 15) // one unit is 80ms
	var down, up time.Duration
	for _, step := range plan {
		if step.down {
			down += step.dur
		} else {
			up += step.dur
		}
	}
	// Key down: S (3 dots) + O (3 dashes) + S = 15 units = 1200ms.
	// Key up: 6 symbol gaps + 2 letter gaps = 12 units = 960ms.
	if down != 1200*time.Millisecond || up != 960*time.Millisecond {
		t.Fatalf("down=%v up=%v, want 1.2s / 960ms", down, up)
	}
	if last := plan[len(plan)-1]; last.down {
		t.Fatal("plan must end with the key up")
	}
}

func TestKeyPlanWordGap(t *testing.T) {
	plan := keyPlan("e e", 15)
	// dot, 7-unit gap, dot, trailing up
	if len(plan) != 4 || plan[1].down || plan[1].dur != 7*80*time.Millisecond {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	plan := keyPlan("HELLO WORLD 73", 20)
	text, raw, _ := decodeMorse(events(plan, exactTiming))
	if text != "HELLO WORLD 73" {
		t.Fatalf("decoded %q (raw %q)", text, raw)
	}
}

func TestDecodeSloppyFist(t *testing.T) {
	// Short dashes, long dots and uneven gaps, like a human on a phone.
	plan := keyPlan("SOS", 12)
	factors := []float64{1.3, 0.8, 1.1, 0.9, 1.4}
	stepIndex := 0
	sloppy := func(step keyStep) time.Duration {
		stepIndex++
		return time.Duration(float64(step.dur) * factors[stepIndex%len(factors)])
	}
	text, raw, unit := decodeMorse(events(plan, sloppy))
	if text != "SOS" {
		t.Fatalf("decoded %q raw %q unit %v", text, raw, unit)
	}
}

func TestDecodeUnknownAndEmpty(t *testing.T) {
	if text, _, _ := decodeMorse(nil); text != "" {
		t.Fatalf("empty input decoded to %q", text)
	}
	// Eight dots is not a letter: it must come back as ? with the raw dots intact.
	unit := 60 * time.Millisecond
	var plan []keyStep
	for dot := 0; dot < 8; dot++ {
		if dot > 0 {
			plan = append(plan, keyStep{false, unit})
		}
		plan = append(plan, keyStep{true, unit})
	}
	plan = append(plan, keyStep{false, 0})
	text, raw, _ := decodeMorse(events(plan, exactTiming))
	if text != "?" || raw != "........" {
		t.Fatalf("got text %q raw %q", text, raw)
	}
}

// A thumb on a phone: dots near 90ms and dashes near 300ms, but pauses
// between taps near 200ms and between letters near 550ms.
func humanTimed(letters []string) []keyEvent {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	var plan []keyStep
	for letterIndex, code := range letters {
		if letterIndex > 0 {
			plan = append(plan, keyStep{false, ms(550)})
		}
		for symbolIndex, symbol := range code {
			if symbolIndex > 0 {
				plan = append(plan, keyStep{false, ms(200)})
			}
			if symbol == '.' {
				plan = append(plan, keyStep{true, ms(90)})
			} else {
				plan = append(plan, keyStep{true, ms(300)})
			}
		}
	}
	plan = append(plan, keyStep{false, 0})
	return events(plan, exactTiming)
}

func TestDecodeHumanThumb(t *testing.T) {
	text, raw, _ := decodeMorse(humanTimed([]string{"...", "---", "..."}))
	if text != "SOS" {
		t.Fatalf("decoded %q raw %q", text, raw)
	}
}
