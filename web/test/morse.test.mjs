import test from 'node:test';
import assert from 'node:assert/strict';
import { createDecoder } from '../static/js/morse.js';

// Drives the decoder with a fake clock; timers fire when the clock passes them.
function harness() {
  let clock = 0;
  let timers = [];
  let latest = { text: '', letter: '' };
  const decoder = createDecoder({
    onChange: state => { latest = state; },
    now: () => clock,
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
  // Keys a letter with explicit press/gap lengths.
  const key = (symbols, { dot, dash, gap }) => {
    for (const [index, symbol] of [...symbols].entries()) {
      if (index) advance(gap);
      decoder.keyDown();
      advance(symbol === '-' ? dash : dot);
      decoder.keyUp();
    }
  };
  return { decoder, advance, key, state: () => latest };
}

const machine = { dot: 60, dash: 180, gap: 60 };
// A thumb on a phone: quick taps, but pauses between taps two to three dots long.
const thumb = { dot: 90, dash: 300, gap: 200 };

test('machine timing: letters, words, trailing letter', () => {
  const { advance, key, state } = harness();
  key('...', machine); advance(180);
  key('---', machine); advance(180);
  key('...', machine); advance(420);
  key('.', machine); advance(180);
  key('.', machine); advance(180);
  assert.equal(state().text, 'SOS EE');
  assert.equal(state().letter, '');
});

test('thumb timing: gaps between taps do not split letters', () => {
  const { advance, key, state } = harness();
  key('...', thumb); advance(550);
  key('---', thumb); advance(550);
  key('...', thumb); advance(1200);
  assert.equal(state().text, 'SOS');
});

test('shows the letter in progress before it is complete', () => {
  const { key, state } = harness();
  key('..', thumb);
  assert.equal(state().letter, '..');
  assert.equal(state().text, '');
});

test('early symbols are re-read once a dot gives the scale', () => {
  const { decoder, advance, key, state } = harness();
  // A session opening with a lone dash has nothing to compare it to.
  key('-', thumb);
  assert.equal(state().letter, '.');
  // The dot that follows sets the scale and the dash is re-read.
  advance(thumb.gap);
  key('.', thumb);
  assert.equal(state().letter, '-.');
  advance(1200);
  assert.equal(state().text, 'N');
  decoder.reset();
});

test('reset clears everything', () => {
  const { decoder, key, state } = harness();
  key('.-', machine);
  decoder.reset();
  assert.deepEqual(state(), { text: '', letter: '' });
});
