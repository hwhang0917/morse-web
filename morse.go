package main

import (
	"strings"
	"time"
)

// Morse encoding and decoding, used by the MCP tools so an agent can send
// text and read what a human keys back. The browser never uses this: humans
// key by hand.

var morseCode = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.",
	'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.",
	'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-", 'U': "..-",
	'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
	'.': ".-.-.-", ',': "--..--", '?': "..--..", '/': "-..-.", '=': "-...-",
}

var morseText = func() map[string]rune {
	m := make(map[string]rune, len(morseCode))
	for r, c := range morseCode {
		m[c] = r
	}
	return m
}()

// Standard timing: dot 1 unit, dash 3, symbol gap 1, letter gap 3, word gap 7.
const (
	dashUnits      = 3
	letterGapUnits = 3
	wordGapUnits   = 7
	// One unit at W words per minute is 1200/W ms (the PARIS convention).
	unitMsPerWPM = 1200
)

func morseUnit(wpm int) time.Duration {
	return time.Duration(unitMsPerWPM/wpm) * time.Millisecond
}

type keyStep struct {
	down bool
	dur  time.Duration
}

// keyPlan turns text into timed key states. Characters without a code are
// skipped. The plan always ends with the key up.
func keyPlan(text string, wpm int) []keyStep {
	unit := morseUnit(wpm)
	var plan []keyStep
	for wi, word := range strings.Fields(strings.ToUpper(text)) {
		if wi > 0 {
			plan = append(plan, keyStep{false, wordGapUnits * unit})
		}
		letters := 0
		for _, r := range word {
			code, ok := morseCode[r]
			if !ok {
				continue
			}
			if letters > 0 {
				plan = append(plan, keyStep{false, letterGapUnits * unit})
			}
			letters++
			for si, s := range code {
				if si > 0 {
					plan = append(plan, keyStep{false, unit})
				}
				d := unit
				if s == '-' {
					d = dashUnits * unit
				}
				plan = append(plan, keyStep{true, d})
			}
		}
	}
	return append(plan, keyStep{false, 0})
}

type keyEvent struct {
	down bool
	at   time.Time
}

// decodeMorse classifies a human's key timings into text. The unit is taken
// from the shortest press or gap seen, then presses over 2 units are dashes,
// gaps over 2 units end a letter and gaps over 5 units end a word. Unknown
// patterns decode to '?'; raw keeps the dots and dashes for the caller.
// ponytail: min-based unit estimate; a lone dash with no gaps decodes as a
// dot. Fine for conversation, use a clustering pass if it ever matters.
func decodeMorse(ev []keyEvent) (text, raw string, unit time.Duration) {
	type run struct {
		down bool
		dur  time.Duration
	}
	var runs []run
	for i := 1; i < len(ev); i++ {
		if ev[i].down == ev[i-1].down {
			continue // duplicate transition; ignore
		}
		runs = append(runs, run{ev[i-1].down, ev[i].at.Sub(ev[i-1].at)})
	}
	for _, r := range runs {
		if r.dur > 0 && (unit == 0 || r.dur < unit) {
			unit = r.dur
		}
	}
	if unit == 0 {
		return "", "", 0
	}

	var out, rawB strings.Builder
	var letter strings.Builder
	flush := func() {
		if letter.Len() == 0 {
			return
		}
		if r, ok := morseText[letter.String()]; ok {
			out.WriteRune(r)
		} else {
			out.WriteByte('?')
		}
		letter.Reset()
	}
	for _, r := range runs {
		units := float64(r.dur) / float64(unit)
		if r.down {
			if units > 2 {
				letter.WriteByte('-')
				rawB.WriteByte('-')
			} else {
				letter.WriteByte('.')
				rawB.WriteByte('.')
			}
			continue
		}
		switch {
		case units > 5:
			flush()
			out.WriteByte(' ')
			rawB.WriteString(" / ")
		case units > 2:
			flush()
			rawB.WriteByte(' ')
		}
	}
	flush()
	return strings.TrimSpace(out.String()), strings.TrimSpace(rawB.String()), unit
}
