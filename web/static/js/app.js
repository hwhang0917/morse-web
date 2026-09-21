import { settings, bindSettings } from './settings.js';
import { audioContext, localTone, remoteTone, vibrate } from './audio.js';
import { cookieNick, registerNick, createSocket } from './socket.js';
import { SYMBOL, createDecoder } from './morse.js';
import { createPlayer } from './player.js';

const TOAST_MS = 3000;
const TRANSLATION_CHARS = 40;
// Sender-side classification. A tap is a dot; holding past DASH_MS is a dash.
// Silence after a symbol ends the letter, then the word.
const DASH_MS = 200;
const LETTER_GAP_MS = 400;
const WORD_GAP_MS = 1000;
// Playback speed for what the partner sends: 80ms is 15 WPM.
const PLAYBACK_UNIT_MS = 80;

const $name = document.querySelector('#nick .name');
const $tag = document.querySelector('#nick .tag');
const $dot = document.querySelector('#nick .dot');
const $status = document.getElementById('status');
const $key = document.getElementById('key');
const $toast = document.getElementById('toast');
const $menu = document.getElementById('menu');
const $online = document.getElementById('online');
const $newNick = document.getElementById('newnick');
const $vibrateSetting = document.getElementById('vib');
const $translation = document.getElementById('translation');
const $translated = document.querySelector('#translation .text');
const $partial = document.querySelector('#translation .partial');

let nick = '';
let partner = '';
let status = 'connecting…';
let keyDown = false;
let keyDownAt = 0;
let letterTimer = null;
let wordTimer = null;
let toastTimer = null;

function toast(text, kind) {
  $toast.textContent = text;
  $toast.className = `show ${kind}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => $toast.classList.remove('show'), TOAST_MS);
}

const decoder = createDecoder({
  onChange({ text, letter }) {
    $translated.textContent = text.slice(-TRANSLATION_CHARS);
    $partial.textContent = letter;
  },
});

// Incoming symbols drive the remote tone, vibration and flash through the
// player so they keep proper rhythm; translation follows the playback.
const player = createPlayer({
  unitMs: PLAYBACK_UNIT_MS,
  onSignal: receive,
  onSymbol(symbol) {
    if (settings.translate) decoder.push(symbol);
  },
});

function render() {
  $dot.classList.toggle('live', Boolean(partner));
  $translation.hidden = !settings.translate;
  const audioBlocked = settings.sound && audioContext.state !== 'running';
  $status.textContent = audioBlocked ? `${status} · tap the key to enable audio` : status;
}

function showNick() {
  const separator = nick.lastIndexOf('_');
  $name.textContent = nick.slice(0, separator);
  $tag.textContent = `(${nick.slice(separator + 1)})`;
}

function receive(on) {
  remoteTone(on);
  if (settings.vibrate) vibrate(on);
  if (settings.flash) document.body.classList.toggle('rx', on);
}

function cancelGapTimers() {
  clearTimeout(letterTimer);
  clearTimeout(wordTimer);
}

// Only transitions count, which guards multi-touch and the keyboard path.
function setKey(isDown) {
  if (isDown === keyDown) return;
  keyDown = isDown;
  document.body.classList.toggle('tx', keyDown);
  localTone(keyDown);

  if (keyDown) {
    cancelGapTimers();
    keyDownAt = performance.now();
    return;
  }
  const held = performance.now() - keyDownAt;
  socket.sendSymbol(held >= DASH_MS ? SYMBOL.DASH : SYMBOL.DOT);
  letterTimer = setTimeout(() => socket.sendSymbol(SYMBOL.LETTER_GAP), LETTER_GAP_MS);
  wordTimer = setTimeout(() => socket.sendSymbol(SYMBOL.WORD_GAP), WORD_GAP_MS);
}

function clearPartner(newStatus) {
  player.clear();
  receive(false);
  decoder.reset();
  partner = '';
  status = newStatus;
  render();
}

function socketParams() {
  return settings.human ? { human: '1' } : {};
}

function reconnect() {
  socket.stop();
  cancelGapTimers();
  clearPartner('connecting…');
  socket.connect(nick, socketParams());
}

function stopOutput(setting) {
  if (setting === 'sound') {
    localTone(false);
    remoteTone(false);
  }
  if (setting === 'vibrate') vibrate(false);
  if (setting === 'flash') document.body.classList.remove('rx');
  if (setting === 'translate') decoder.reset();
}

const socket = createSocket({
  onSymbol: player.enqueue,
  onWaiting() {
    if (partner) toast(`${partner} disconnected`, 'bad');
    clearPartner('waiting for a partner…');
  },
  onPaired(partnerNick, isBot) {
    partner = partnerNick;
    const label = isBot ? `${partnerNick} (bot)` : partnerNick;
    toast(`connected to ${label}`, 'ok');
    status = `connected to ${label}`;
    render();
  },
  onOnline(count) {
    $online.textContent = `${count} online`;
  },
  onClose() {
    $online.textContent = '';
    setKey(false);
    cancelGapTimers();
    clearPartner('disconnected, retrying…');
  },
});

audioContext.onstatechange = render;

$key.addEventListener('pointerdown', event => {
  $key.setPointerCapture(event.pointerId);
  audioContext.resume();
  setKey(true);
});
$key.addEventListener('pointerup', () => setKey(false));
$key.addEventListener('pointercancel', () => setKey(false));
$key.addEventListener('contextmenu', event => event.preventDefault());

window.addEventListener('keydown', event => {
  if (event.code !== 'Space' || event.repeat || event.target.closest('#menu')) return;
  event.preventDefault();
  audioContext.resume();
  setKey(true);
});
window.addEventListener('keyup', event => {
  if (event.code === 'Space') setKey(false);
});
// Alt-tabbing with the key held would otherwise leave the tone on.
window.addEventListener('blur', () => setKey(false));
document.addEventListener('visibilitychange', () => {
  if (document.hidden) setKey(false);
});

// The drawer backdrop is a pseudo-element of #menu itself, so only the panel
// and its button count as "inside".
document.addEventListener('pointerdown', event => {
  if ($menu.open && !event.target.closest('#menu > div, #menu > summary')) $menu.open = false;
});

$vibrateSetting.hidden = !navigator.vibrate;
bindSettings((setting, enabled) => {
  if (!enabled) stopOutput(setting);
  if (setting === 'human') reconnect();
  render();
});

$newNick.addEventListener('click', async () => {
  try {
    nick = await registerNick();
  } catch {
    toast('too many nickname requests, try again later', 'bad');
    return;
  }
  showNick();
  reconnect();
});

(async () => {
  nick = cookieNick() || await registerNick();
  showNick();
  render();
  socket.connect(nick, socketParams());
})();
