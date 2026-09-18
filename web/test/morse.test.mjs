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
    clock += ms;
    for (const timer of timers.filter(timer => timer.at <= clock)) {
      timers = timers.filter(other => other !== timer);
      timer.fn();
    }
  };
  const key = (symbols, unit) => {
    for (const [index, symbol] of [...symbols].entries()) {
      if (index) advance(unit);
      decoder.keyDown();
      advance(symbol === '-' ? unit * 3 : unit);
      decoder.keyUp();
    }
  };
  return { decoder, advance, key, state: () => latest };
}

test('decodes letters, words and prosigns at exact timing', () => {
  const { advance, key, state } = harness();
  const unit = 60;
  key('...', unit); advance(unit * 3);
  key('---', unit); advance(unit * 3);
  key('...', unit); advance(unit * 7);
  key('.', unit); advance(unit * 3);
  key('.', unit); advance(unit * 3);
  assert.equal(state().text, 'SOS EE');
  assert.equal(state().letter, '');
});

test('shows the letter in progress before it is complete', () => {
  const { key, state } = harness();
  key('..', 60);
  assert.equal(state().letter, '..');
  assert.equal(state().text, '');
});

test('tolerates a sloppy fist', () => {
  const { decoder, advance, state } = harness();
  const press = ms => { decoder.keyDown(); advance(ms); decoder.keyUp(); };
  press(70); advance(55); press(90); advance(60); press(65);   // S
  advance(210);
  press(250); advance(70); press(200); advance(80); press(230); // O
  advance(400);
  assert.equal(state().text, 'SO');
});

test('reset clears everything', () => {
  const { decoder, key, state } = harness();
  key('.-', 60);
  decoder.reset();
  assert.deepEqual(state(), { text: '', letter: '' });
});
