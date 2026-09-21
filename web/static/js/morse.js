// Wire symbols, one byte each; the same values the server relays.
export const SYMBOL = { DOT: 0, DASH: 1, LETTER_GAP: 2, WORD_GAP: 3 };

const LETTERS = {
  '.-': 'A', '-...': 'B', '-.-.': 'C', '-..': 'D', '.': 'E', '..-.': 'F', '--.': 'G',
  '....': 'H', '..': 'I', '.---': 'J', '-.-': 'K', '.-..': 'L', '--': 'M', '-.': 'N',
  '---': 'O', '.--.': 'P', '--.-': 'Q', '.-.': 'R', '...': 'S', '-': 'T', '..-': 'U',
  '...-': 'V', '.--': 'W', '-..-': 'X', '-.--': 'Y', '--..': 'Z',
  '-----': '0', '.----': '1', '..---': '2', '...--': '3', '....-': '4',
  '.....': '5', '-....': '6', '--...': '7', '---..': '8', '----.': '9',
  '.-.-.-': '.', '--..--': ',', '..--..': '?', '-..-.': '/', '-...-': '=',
};

// Turns incoming symbols into text. Timing never enters into it: the sender
// already decided what was a dot, a dash, or the end of a letter.
export function createDecoder({ onChange }) {
  let text = '';
  let letter = '';

  const emit = () => onChange({ text, letter });

  function endLetter() {
    if (!letter) return;
    text += LETTERS[letter] ?? '?';
    letter = '';
  }

  function push(symbol) {
    switch (symbol) {
      case SYMBOL.DOT:
        letter += '.';
        break;
      case SYMBOL.DASH:
        letter += '-';
        break;
      case SYMBOL.LETTER_GAP:
        endLetter();
        break;
      case SYMBOL.WORD_GAP:
        endLetter();
        if (text && !text.endsWith(' ')) text += ' ';
        break;
    }
    emit();
  }

  function reset() {
    text = '';
    letter = '';
    emit();
  }

  return { push, reset };
}
