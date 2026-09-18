package main

import (
	"strings"
	"time"
)

// Used by the MCP tools only; humans key by hand in the browser.

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
	table := make(map[string]rune, len(morseCode))
	for letter, code := range morseCode {
		table[code] = letter
	}
	return table
}()

// Standard timing: dot 1 unit, dash 3, symbol gap 1, letter gap 3, word gap 7.
const (
	dashUnits      = 3
	letterGapUnits = 3
	wordGapUnits   = 7
	unitMsPerWPM   = 1200 // PARIS convention: one unit at W words per minute is 1200/W ms
)

func morseUnit(wpm int) time.Duration {
	return time.Duration(unitMsPerWPM/wpm) * time.Millisecond
}

type keyStep struct {
	down bool
	dur  time.Duration
}

// keyPlan turns text into timed key states. Characters without a code are
// skipped and the plan always ends with the key up.
func keyPlan(text string, wpm int) []keyStep {
	unit := morseUnit(wpm)
	var plan []keyStep
	for wordIndex, word := range strings.Fields(strings.ToUpper(text)) {
		if wordIndex > 0 {
			plan = append(plan, keyStep{false, wordGapUnits * unit})
		}
		letters := 0
		for _, letter := range word {
			code, known := morseCode[letter]
			if !known {
				continue
			}
			if letters > 0 {
				plan = append(plan, keyStep{false, letterGapUnits * unit})
			}
			letters++
			for symbolIndex, symbol := range code {
				if symbolIndex > 0 {
					plan = append(plan, keyStep{false, unit})
				}
				duration := unit
				if symbol == '-' {
					duration = dashUnits * unit
				}
				plan = append(plan, keyStep{true, duration})
			}
		}
	}
	return append(plan, keyStep{false, 0})
}

type keyEvent struct {
	down bool
	at   time.Time
}

// Thresholds for classifying a partner's timings.
const (
	dashRatio      = 2   // press longer than this many dots is a dash
	letterGapRatio = 2.5 // gap longer than this many reference units ends a letter
	wordGapRatio   = 6   // gap longer than this many reference units ends a word
)

// decodeMorse classifies key timings into text, best effort. Presses are
// judged against the shortest press (a dot). Gaps are judged against the
// larger of the shortest press and the shortest gap: a thumb's pauses between
// taps run two to three times its dots, so measuring gaps in dots would split
// every letter. Machine timing has equal dots and symbol gaps, so it is
// unaffected. Unknown patterns become '?'.
// ponytail: minimum-based estimates, so a session that opens with a lone dash
// reads it as a dot until a real dot arrives. Use clustering if it matters.
func decodeMorse(events []keyEvent) (text, raw string, dot time.Duration) {
	type segment struct {
		down bool
		dur  time.Duration
	}
	var segments []segment
	for index := 1; index < len(events); index++ {
		previous, current := events[index-1], events[index]
		if current.down == previous.down {
			continue
		}
		segments = append(segments, segment{previous.down, current.at.Sub(previous.at)})
	}

	var shortestGap time.Duration
	for _, seg := range segments {
		if seg.dur <= 0 {
			continue
		}
		if seg.down && (dot == 0 || seg.dur < dot) {
			dot = seg.dur
		}
		if !seg.down && (shortestGap == 0 || seg.dur < shortestGap) {
			shortestGap = seg.dur
		}
	}
	if dot == 0 {
		return "", "", 0
	}
	gapUnit := max(dot, shortestGap)

	var textOut, rawOut, letter strings.Builder
	endLetter := func() {
		if letter.Len() == 0 {
			return
		}
		if decoded, known := morseText[letter.String()]; known {
			textOut.WriteRune(decoded)
		} else {
			textOut.WriteByte('?')
		}
		letter.Reset()
	}
	for _, seg := range segments {
		switch {
		case seg.down && float64(seg.dur) > dashRatio*float64(dot):
			letter.WriteByte('-')
			rawOut.WriteByte('-')
		case seg.down:
			letter.WriteByte('.')
			rawOut.WriteByte('.')
		case float64(seg.dur) > wordGapRatio*float64(gapUnit):
			endLetter()
			textOut.WriteByte(' ')
			rawOut.WriteString(" / ")
		case float64(seg.dur) > letterGapRatio*float64(gapUnit):
			endLetter()
			rawOut.WriteByte(' ')
		}
	}
	endLetter()
	return strings.TrimSpace(textOut.String()), strings.TrimSpace(rawOut.String()), dot
}
