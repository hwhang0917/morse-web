package main

import "strings"

// Used by the MCP tools only; the browser has its own copy of the table.

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

// encodeSymbols turns text into wire symbols. Characters without a code are
// skipped; letters end with opLetterGap and words with opWordGap.
func encodeSymbols(text string) []byte {
	var symbols []byte
	for wordIndex, word := range strings.Fields(strings.ToUpper(text)) {
		if wordIndex > 0 {
			symbols = append(symbols, opWordGap)
		}
		letters := 0
		for _, letter := range word {
			code, known := morseCode[letter]
			if !known {
				continue
			}
			if letters > 0 {
				symbols = append(symbols, opLetterGap)
			}
			letters++
			for _, mark := range code {
				if mark == '-' {
					symbols = append(symbols, opDash)
				} else {
					symbols = append(symbols, opDot)
				}
			}
		}
	}
	if len(symbols) > 0 {
		symbols = append(symbols, opLetterGap)
	}
	return symbols
}

// decodeSymbols turns wire symbols into text. Unknown letters become '?';
// raw keeps the dots and dashes with letters space-separated and words
// separated by " / ".
func decodeSymbols(symbols []byte) (text, raw string) {
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
		rawOut.WriteByte(' ')
	}
	for _, op := range symbols {
		switch op {
		case opDot:
			letter.WriteByte('.')
			rawOut.WriteByte('.')
		case opDash:
			letter.WriteByte('-')
			rawOut.WriteByte('-')
		case opLetterGap:
			endLetter()
		case opWordGap:
			endLetter()
			if out := textOut.String(); out != "" && !strings.HasSuffix(out, " ") {
				textOut.WriteByte(' ')
				rawOut.WriteString("/ ")
			}
		}
	}
	endLetter()
	return strings.TrimSpace(textOut.String()), strings.TrimSpace(rawOut.String())
}
