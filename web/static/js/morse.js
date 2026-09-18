const LETTERS = {
  '.-': 'A', '-...': 'B', '-.-.': 'C', '-..': 'D', '.': 'E', '..-.': 'F', '--.': 'G',
  '....': 'H', '..': 'I', '.---': 'J', '-.-': 'K', '.-..': 'L', '--': 'M', '-.': 'N',
  '---': 'O', '.--.': 'P', '--.-': 'Q', '.-.': 'R', '...': 'S', '-': 'T', '..-': 'U',
  '...-': 'V', '.--': 'W', '-..-': 'X', '-.--': 'Y', '--..': 'Z',
  '-----': '0', '.----': '1', '..---': '2', '...--': '3', '....-': '4',
  '.....': '5', '-....': '6', '--...': '7', '---..': '8', '----.': '9',
  '.-.-.-': '.', '--..--': ',', '..--..': '?', '-..-.': '/', '-...-': '=',
};

// Same rules as the server's decoder. Presses are judged against the shortest
// press (a dot). Gaps are judged against the larger of the shortest press and
// the shortest gap, because a thumb pauses two to three dots between taps.
const DASH_RATIO = 2;
const LETTER_GAP_RATIO = 2.5;
const WORD_GAP_RATIO = 6;
// Presses or gaps shorter than this are touch glitches, not signal.
const MIN_CREDIBLE_MS = 30;

export function decode(events, now) {
  const segments = [];
  for (let index = 1; index < events.length; index++) {
    const previous = events[index - 1];
    const current = events[index];
    if (current.down !== previous.down) segments.push({ down: previous.down, ms: current.at - previous.at });
  }

  let dot = 0;
  let shortestGap = 0;
  for (const { down, ms } of segments) {
    if (ms < MIN_CREDIBLE_MS) continue;
    if (down && (dot === 0 || ms < dot)) dot = ms;
    if (!down && (shortestGap === 0 || ms < shortestGap)) shortestGap = ms;
  }
  if (dot === 0) return { text: '', letter: '', letterEndsIn: 0 };
  const gapUnit = Math.max(dot, shortestGap);

  let text = '';
  let letter = '';
  const endLetter = () => {
    if (!letter) return;
    text += LETTERS[letter] ?? '?';
    letter = '';
  };
  for (const { down, ms } of segments) {
    if (down) {
      letter += ms > DASH_RATIO * dot ? '-' : '.';
    } else if (ms > WORD_GAP_RATIO * gapUnit) {
      endLetter();
      text += ' ';
    } else if (ms > LETTER_GAP_RATIO * gapUnit) {
      endLetter();
    }
  }

  // The trailing letter is complete once the silence after it is long enough.
  const last = events[events.length - 1];
  const letterEnd = LETTER_GAP_RATIO * gapUnit;
  const silence = last && !last.down ? now - last.at : 0;
  if (silence > letterEnd) endLetter();
  return { text, letter, letterEndsIn: letter ? Math.max(0, letterEnd - silence) : 0 };
}

// Live wrapper: re-decodes the whole session on every key event so early
// letters are corrected as the partner's timing becomes clear.
export function createDecoder({
  onChange,
  now = () => performance.now(),
  schedule = setTimeout,
  cancel = clearTimeout,
} = {}) {
  let events = [];
  let timer = null;

  function refresh() {
    cancel(timer);
    const { text, letter, letterEndsIn } = decode(events, now());
    onChange({ text, letter });
    if (letter) timer = schedule(refresh, letterEndsIn + 1);
  }

  function record(down) {
    events.push({ down, at: now() });
    refresh();
  }

  function reset() {
    cancel(timer);
    events = [];
    onChange({ text: '', letter: '' });
  }

  return { keyDown: () => record(true), keyUp: () => record(false), reset };
}
