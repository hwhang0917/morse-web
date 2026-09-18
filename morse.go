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

// decodeMorse classifies a human's key timings into text. The unit is the
// shortest press or gap seen; presses over 2 units are dashes, gaps over 2
// units end a letter and gaps over 5 end a word. Unknown patterns become '?'.
// ponytail: min-based unit estimate, so a lone dash with no gaps decodes as
// a dot. Fine for conversation; use a clustering pass if it ever matters.
func decodeMorse(events []keyEvent) (text, raw string, unit time.Duration) {
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
	for _, seg := range segments {
		if seg.dur > 0 && (unit == 0 || seg.dur < unit) {
			unit = seg.dur
		}
	}
	if unit == 0 {
		return "", "", 0
	}

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
		units := float64(seg.dur) / float64(unit)
		switch {
		case seg.down && units > 2:
			letter.WriteByte('-')
			rawOut.WriteByte('-')
		case seg.down:
			letter.WriteByte('.')
			rawOut.WriteByte('.')
		case units > 5:
			endLetter()
			textOut.WriteByte(' ')
			rawOut.WriteString(" / ")
		case units > 2:
			endLetter()
			rawOut.WriteByte(' ')
		}
	}
	endLetter()
	return strings.TrimSpace(textOut.String()), strings.TrimSpace(rawOut.String()), unit
}
