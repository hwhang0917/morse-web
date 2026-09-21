import test from 'node:test';
import assert from 'node:assert/strict';
import { SYMBOL, createDecoder } from '../static/js/morse.js';

function decoderHarness() {
  let latest = { text: '', letter: '' };
  const decoder = createDecoder({ onChange: state => { latest = state; } });
  const feed = symbols => symbols.forEach(symbol => decoder.push(symbol));
  return { decoder, feed, state: () => latest };
}

const { DOT, DASH, LETTER_GAP, WORD_GAP } = SYMBOL;

test('decodes letters and words', () => {
  const { feed, state } = decoderHarness();
  feed([DOT, DOT, DOT, LETTER_GAP, DASH, DASH, DASH, LETTER_GAP, DOT, DOT, DOT, LETTER_GAP, WORD_GAP, DOT, LETTER_GAP]);
  assert.equal(state().text, 'SOS E');
  assert.equal(state().letter, '');
});

test('shows the letter in progress', () => {
  const { feed, state } = decoderHarness();
  feed([DASH, DOT]);
  assert.deepEqual(state(), { text: '', letter: '-.' });
});

test('unknown pattern becomes a question mark and word gaps do not double', () => {
  const { feed, state } = decoderHarness();
  feed([DOT, DOT, DOT, DOT, DOT, DOT, DOT, DOT, LETTER_GAP, WORD_GAP, WORD_GAP, DOT, LETTER_GAP]);
  assert.equal(state().text, '? E');
});

test('reset clears everything', () => {
  const { decoder, feed, state } = decoderHarness();
  feed([DOT, DASH]);
  decoder.reset();
  assert.deepEqual(state(), { text: '', letter: '' });
});
