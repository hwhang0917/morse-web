// Nickname registration and the WebSocket to the hub.

const RECONNECT_MS = 2000;
const NICK_COOKIE_MAX_AGE_S = 365 * 24 * 3600;
const OP = { KEY_UP: 0x00, KEY_DOWN: 0x01, WAITING: 0x10, PAIRED: 0x11, ONLINE: 0x20 };
const FLAG_BOT = 0x01; // flags byte of a PAIRED frame
// Mirrors nickRE on the server; a stale cookie that fails it would 400 forever.
const NICK_RE = /^[a-z]{1,20}_[a-z]{1,20}_[a-z0-9]{4}$/;

// The nickname lives in a cookie so it survives closing the tab.
export function cookieNick() {
  const m = document.cookie.match(/(?:^|; )nick=([^;]*)/);
  return m && NICK_RE.test(m[1]) ? m[1] : '';
}

export async function registerNick() {
  const r = await fetch('/api/register', { method: 'POST' });
  if (!r.ok) throw new Error(`register: ${r.status}`);
  const n = (await r.json()).nick;
  const secure = location.protocol === 'https:' ? '; Secure' : '';
  document.cookie = `nick=${n}; Max-Age=${NICK_COOKIE_MAX_AGE_S}; Path=/; SameSite=Lax${secure}`;
  return n;
}

// createSocket returns a connection that reconnects itself until stop() is
// called. Handlers: onKey(down), onWaiting(), onPaired(partnerNick, isBot),
// onOnline(count), onClose().
export function createSocket({ onKey, onWaiting, onPaired, onOnline, onClose }) {
  let ws, retryTimer, nick;

  function connect(newNick) {
    if (newNick !== undefined) nick = newNick;
    const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
    const sock = new WebSocket(`${scheme}://${location.host}/ws?nick=${nick}`);
    sock.binaryType = 'arraybuffer';
    sock.onmessage = e => {
      const b = new Uint8Array(e.data);
      switch (b[0]) {
        case OP.KEY_DOWN: onKey(true); break;
        case OP.KEY_UP: onKey(false); break;
        case OP.WAITING: onWaiting(); break;
        case OP.PAIRED: onPaired(new TextDecoder().decode(b.subarray(2)), (b[1] & FLAG_BOT) !== 0); break;
        case OP.ONLINE: onOnline((b[1] << 8) | b[2]); break;
      }
    };
    sock.onclose = () => {
      onClose();
      retryTimer = setTimeout(connect, RECONNECT_MS);
    };
    ws = sock;
  }

  // Drop the current socket without triggering its retry (e.g. before a nick change).
  function stop() {
    clearTimeout(retryTimer);
    if (ws) { ws.onclose = null; ws.close(); }
  }

  function sendKey(down) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(new Uint8Array([down ? OP.KEY_DOWN : OP.KEY_UP]));
  }

  return { connect, stop, sendKey };
}
