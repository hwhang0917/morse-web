import { settings, bindSettings } from '/settings.js';
import { ac, localTone, remoteTone, vibrate } from '/audio.js';
import { cookieNick, registerNick, createSocket } from '/socket.js';

const TOAST_MS = 3000;

const $name = document.querySelector('#nick .name');
const $tag = document.querySelector('#nick .tag');
const $dot = document.querySelector('#nick .dot');
const $status = document.getElementById('status');
const $key = document.getElementById('key');
const $toast = document.getElementById('toast');
const $menu = document.getElementById('menu');

let nick = '', partner = '', state = 'connecting…', down = false;

// ---- UI ----
let toastTimer;
function toast(text, kind) {
  $toast.textContent = text;
  $toast.className = `show ${kind}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => $toast.classList.remove('show'), TOAST_MS);
}

function render() {
  $dot.classList.toggle('live', !!partner);
  const needAudio = settings.sound && ac.state !== 'running';
  $status.textContent = state + (needAudio ? ' · tap the key to enable audio' : '');
}
ac.onstatechange = render;

function showNick() {
  // sleepy_beaver_jf34 -> "sleepy_beaver" highlighted, "(jf34)" dimmed.
  const i = nick.lastIndexOf('_');
  $name.textContent = nick.slice(0, i);
  $tag.textContent = '(' + nick.slice(i + 1) + ')';
}

// Tapping anywhere outside the panel closes it. The drawer backdrop is a
// pseudo-element of #menu itself, so test against the panel and summary only.
document.addEventListener('pointerdown', e => {
  if ($menu.open && !e.target.closest('#menu > div, #menu summary')) $menu.open = false;
});

// ---- settings ----
if (navigator.vibrate) document.getElementById('vib').hidden = false;
bindSettings((key, on) => {
  // Turning something off mid-signal must also stop the signal.
  if (!on) {
    if (key === 'sound') { localTone(false); remoteTone(false); }
    if (key === 'vibrate') vibrate(false);
    if (key === 'flash') document.body.classList.remove('rx');
  }
  render();
});

// ---- incoming signal ----
function rx(on) {
  remoteTone(on);
  if (settings.vibrate) vibrate(on);
  if (settings.flash) document.body.classList.toggle('rx', on);
}

// ---- outgoing key: only transitions are sent, which guards multi-touch and the keyboard path. ----
function setKey(isDown) {
  if (isDown === down) return;
  down = isDown;
  document.body.classList.toggle('tx', down);
  localTone(down);
  socket.sendKey(down);
}

$key.addEventListener('pointerdown', e => { $key.setPointerCapture(e.pointerId); ac.resume(); setKey(true); });
$key.addEventListener('pointerup', () => setKey(false));
$key.addEventListener('pointercancel', () => setKey(false));
$key.addEventListener('contextmenu', e => e.preventDefault());
window.addEventListener('keydown', e => {
  if (e.code !== 'Space' || e.repeat || e.target.closest('#menu')) return;
  e.preventDefault();
  ac.resume();
  setKey(true);
});
window.addEventListener('keyup', e => { if (e.code === 'Space') setKey(false); });
// A held key while alt-tabbing would otherwise leave the partner's tone on.
window.addEventListener('blur', () => setKey(false));
document.addEventListener('visibilitychange', () => { if (document.hidden) setKey(false); });

// ---- socket ----
const socket = createSocket({
  onKey: rx,
  onWaiting() {
    rx(false);
    // The same frame also means "partner left"; only toast when there was one.
    if (partner) toast(`${partner} disconnected`, 'bad');
    partner = '';
    state = 'waiting for a partner…';
    render();
  },
  onPaired(p) {
    partner = p;
    toast(`connected to ${partner}`, 'ok');
    state = 'connected to ' + partner;
    render();
  },
  onClose() {
    rx(false);
    setKey(false);
    partner = '';
    state = 'disconnected, retrying…';
    render();
  },
});

document.getElementById('newnick').addEventListener('click', async () => {
  try { nick = await registerNick(); }
  catch { toast('too many nickname requests, try again later', 'bad'); return; }
  showNick();
  socket.stop();
  rx(false);
  partner = '';
  state = 'connecting…';
  render();
  socket.connect(nick);
});

(async () => {
  nick = cookieNick() || await registerNick();
  showNick();
  render();
  socket.connect(nick);
})();
