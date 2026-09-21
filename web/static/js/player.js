import { SYMBOL } from './morse.js';

// Standard Morse timing in units: signal on, then silence.
const TIMING = {
  [SYMBOL.DOT]: { on: 1, off: 1 },
  [SYMBOL.DASH]: { on: 3, off: 1 },
  [SYMBOL.LETTER_GAP]: { on: 0, off: 2 }, // 1 after the symbol + 2 = 3
  [SYMBOL.WORD_GAP]: { on: 0, off: 4 },   // 3 after the letter + 4 = 7
};

// Plays incoming symbols back at a steady speed, so what the partner hears
// has proper rhythm no matter how the frames arrived.
export function createPlayer({
  unitMs,
  onSignal,
  onSymbol,
  maxQueue = 200,
  schedule = setTimeout,
  cancel = clearTimeout,
}) {
  let queue = [];
  let playing = false;
  let timer = null;

  function next() {
    playing = queue.length > 0;
    if (!playing) return;
    const symbol = queue.shift();
    const { on, off } = TIMING[symbol];
    onSymbol(symbol);
    if (!on) {
      timer = schedule(next, off * unitMs);
      return;
    }
    onSignal(true);
    timer = schedule(() => {
      onSignal(false);
      timer = schedule(next, off * unitMs);
    }, on * unitMs);
  }

  function enqueue(symbol) {
    if (queue.length >= maxQueue) return;
    queue.push(symbol);
    if (!playing) next();
  }

  function clear() {
    cancel(timer);
    queue = [];
    if (playing) onSignal(false);
    playing = false;
  }

  return { enqueue, clear };
}
