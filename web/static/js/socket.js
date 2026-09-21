const RECONNECT_MS = 2000;
const NICK_COOKIE_MAX_AGE_S = 365 * 24 * 3600;
// Mirrors nickRE on the server; a stale cookie that fails it would be rejected forever.
const NICK_PATTERN = /^[a-z]{1,20}_[a-z]{1,20}_[a-z0-9]{4}$/;

const OP = { DOT: 0x00, DASH: 0x01, LETTER_GAP: 0x02, WORD_GAP: 0x03, WAITING: 0x10, PAIRED: 0x11, ONLINE: 0x20 };
const FLAG_BOT = 0x01;

export function cookieNick() {
  const match = document.cookie.match(/(?:^|; )nick=([^;]*)/);
  const nick = match ? match[1] : '';
  return NICK_PATTERN.test(nick) ? nick : '';
}

export async function registerNick() {
  const response = await fetch('/api/register', { method: 'POST' });
  if (!response.ok) throw new Error(`register: ${response.status}`);
  const { nick } = await response.json();
  const secure = location.protocol === 'https:' ? '; Secure' : '';
  document.cookie = `nick=${nick}; Max-Age=${NICK_COOKIE_MAX_AGE_S}; Path=/; SameSite=Lax${secure}`;
  return nick;
}

export function createSocket({ onSymbol, onWaiting, onPaired, onOnline, onClose }) {
  let socket;
  let retryTimer;
  let nick;
  let params = {};

  function handleFrame(frame) {
    switch (frame[0]) {
      case OP.DOT:
      case OP.DASH:
      case OP.LETTER_GAP:
      case OP.WORD_GAP:
        onSymbol(frame[0]);
        break;
      case OP.WAITING:
        onWaiting();
        break;
      case OP.PAIRED: {
        const partnerNick = new TextDecoder().decode(frame.subarray(2));
        const isBot = (frame[1] & FLAG_BOT) !== 0;
        onPaired(partnerNick, isBot);
        break;
      }
      case OP.ONLINE:
        onOnline((frame[1] << 8) | frame[2]);
        break;
    }
  }

  function connect(newNick, newParams) {
    if (newNick !== undefined) nick = newNick;
    if (newParams !== undefined) params = newParams;

    const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
    const query = new URLSearchParams({ nick, ...params });
    socket = new WebSocket(`${scheme}://${location.host}/ws?${query}`);
    socket.binaryType = 'arraybuffer';
    socket.onmessage = event => handleFrame(new Uint8Array(event.data));
    socket.onclose = () => {
      onClose();
      retryTimer = setTimeout(connect, RECONNECT_MS);
    };
  }

  function stop() {
    clearTimeout(retryTimer);
    if (!socket) return;
    socket.onclose = null;
    socket.close();
  }

  function sendSymbol(op) {
    if (socket?.readyState !== WebSocket.OPEN) return;
    socket.send(new Uint8Array([op]));
  }

  return { connect, stop, sendSymbol };
}
