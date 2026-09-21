package main

import (
	"bytes"
	"testing"
)

func TestEncodeSymbols(t *testing.T) {
	got := encodeSymbols("so s")
	want := []byte{opDot, opDot, opDot, opLetterGap, opDash, opDash, opDash, opWordGap, opDot, opDot, opDot, opLetterGap}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := encodeSymbols("!!!"); len(got) != 0 {
		t.Fatalf("unencodable text should produce no symbols, got %v", got)
	}
}

func TestDecodeSymbolsRoundTrip(t *testing.T) {
	text, raw := decodeSymbols(encodeSymbols("HELLO WORLD 73"))
	if text != "HELLO WORLD 73" {
		t.Fatalf("decoded %q (raw %q)", text, raw)
	}
	if raw != ".... . .-.. .-.. --- / .-- --- .-. .-.. -.. / --... ...--" {
		t.Fatalf("raw %q", raw)
	}
}

func TestDecodeSymbolsEdges(t *testing.T) {
	if text, _ := decodeSymbols(nil); text != "" {
		t.Fatalf("empty input decoded to %q", text)
	}
	// A letter gap followed by a word gap, as the browser sends, is one space.
	text, _ := decodeSymbols([]byte{opDot, opLetterGap, opWordGap, opDot, opLetterGap})
	if text != "E E" {
		t.Fatalf("got %q, want %q", text, "E E")
	}
	// An unfinished letter still decodes; an unknown pattern is a question mark.
	text, raw := decodeSymbols([]byte{opDot, opDot, opDot, opDot, opDot, opDot, opDot, opDot})
	if text != "?" || raw != "........" {
		t.Fatalf("got text %q raw %q", text, raw)
	}
}
