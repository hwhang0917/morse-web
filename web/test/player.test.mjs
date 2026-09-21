import test from 'node:test';
import assert from 'node:assert/strict';
import { SYMBOL } from '../static/js/morse.js';
import { createPlayer } from '../static/js/player.js';

// Fake clock; records every signal change with its time.
function playerHarness() {
  let clock = 0;
  let timers = [];
  const signals = [];
  const symbols = [];
  const player = createPlayer({
    unitMs: 10,
    onSignal: on => signals.push([clock, on]),
    onSymbol: symbol => symbols.push(symbol),
    schedule: (fn, ms) => { const timer = { at: clock + ms, fn }; timers.push(timer); return timer; },
    cancel: timer => { timers = timers.filter(other => other !== timer); },
  });
  const advance = ms => {
    const target = clock + ms;
    while (true) {
      const due = timers.filter(timer => timer.at <= target).sort((a, b) => a.at - b.at)[0];
      if (!due) break;
      timers = timers.filter(other => other !== due);
      clock = due.at;
      due.fn();
    }
    clock = target;
  };
  return { player, advance, signals, symbols };
}

test('plays a dot and a dash with standard timing', () => {
  const { player, advance, signals } = playerHarness();
  player.enqueue(SYMBOL.DOT);
  player.enqueue(SYMBOL.DASH);
  advance(100);
  // dot: on at 0, off at 10; gap 10; dash: on at 20, off at 50
  assert.deepEqual(signals, [[0, true], [10, false], [20, true], [50, false]]);
});

test('queues symbols that arrive during playback instead of overlapping', () => {
  const { player, advance, signals } = playerHarness();
  player.enqueue(SYMBOL.DASH);
  advance(5);
  player.enqueue(SYMBOL.DOT); // arrives mid-dash
  advance(100);
  assert.deepEqual(signals, [[0, true], [30, false], [40, true], [50, false]]);
});

test('gaps add silence and are reported as symbols', () => {
  const { player, advance, signals, symbols } = playerHarness();
  player.enqueue(SYMBOL.DOT);
  player.enqueue(SYMBOL.LETTER_GAP);
  player.enqueue(SYMBOL.DOT);
  advance(100);
  // dot off at 10, +10 symbol gap, +20 letter gap: next dot on at 40
  assert.deepEqual(signals, [[0, true], [10, false], [40, true], [50, false]]);
  assert.deepEqual(symbols, [SYMBOL.DOT, SYMBOL.LETTER_GAP, SYMBOL.DOT]);
});

test('clear stops the signal and drops the queue', () => {
  const { player, advance, signals } = playerHarness();
  player.enqueue(SYMBOL.DASH);
  player.enqueue(SYMBOL.DASH);
  advance(5);
  player.clear();
  advance(100);
  assert.deepEqual(signals, [[0, true], [5, false]]);
});
