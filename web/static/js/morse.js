const LETTERS = {
  '.-': 'A', '-...': 'B', '-.-.': 'C', '-..': 'D', '.': 'E', '..-.': 'F', '--.': 'G',
  '....': 'H', '..': 'I', '.---': 'J', '-.-': 'K', '.-..': 'L', '--': 'M', '-.': 'N',
  '---': 'O', '.--.': 'P', '--.-': 'Q', '.-.': 'R', '...': 'S', '-': 'T', '..-': 'U',
  '...-': 'V', '.--': 'W', '-..-': 'X', '-.--': 'Y', '--..': 'Z',
  '-----': '0', '.----': '1', '..---': '2', '...--': '3', '....-': '4',
  '.....': '5', '-....': '6', '--...': '7', '---..': '8', '----.': '9',
  '.-.-.-': '.', '--..--': ',', '..--..': '?', '-..-.': '/', '-...-': '=',
};

const DASH_RATIO = 2;
const LETTER_GAP_RATIO = 2;
const WORD_GAP_RATIO = 5;
const LETTER_END_RATIO = 3;
const DEFAULT_UNIT_MS = 80;
// Presses shorter than this are touch glitches, not dots, and must not set the unit.
const MIN_UNIT_MS = 30;

// Best-effort live decoding of a partner's key timings. The unit is the
// shortest credible press or gap seen so far, so the first symbols of a
// session may be misread until a dot has been heard.
export function createDecoder({
  onChange,
  now = () => performance.now(),
  schedule = setTimeout,
  cancel = clearTimeout,
} = {}) {
  let text = '';
  let letter = '';
  let unit = 0;
  let downAt = 0;
  let upAt = 0;
  let letterTimer = null;

  const currentUnit = () => unit || DEFAULT_UNIT_MS;

  function learnUnit(ms) {
    if (ms >= MIN_UNIT_MS && (unit === 0 || ms < unit)) unit = ms;
  }

  function emit() {
    onChange({ text, letter });
  }

  function endLetter() {
    if (!letter) return;
    text += LETTERS[letter] ?? '?';
    letter = '';
    emit();
  }

  function keyDown() {
    cancel(letterTimer);
    const at = now();
    if (upAt) {
      const gap = at - upAt;
      learnUnit(gap);
      if (gap > WORD_GAP_RATIO * currentUnit()) {
        endLetter();
        text += ' ';
      } else if (gap > LETTER_GAP_RATIO * currentUnit()) {
        endLetter();
      }
    }
    downAt = at;
  }

  function keyUp() {
    const at = now();
    const press = at - downAt;
    learnUnit(press);
    letter += press > DASH_RATIO * currentUnit() ? '-' : '.';
    upAt = at;
    emit();
    letterTimer = schedule(endLetter, LETTER_END_RATIO * currentUnit());
  }

  function reset() {
    cancel(letterTimer);
    text = '';
    letter = '';
    unit = 0;
    downAt = 0;
    upAt = 0;
    emit();
  }

  return { keyDown, keyUp, reset };
}
