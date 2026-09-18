// Tone and vibration output.
import { settings } from './settings.js';

const TONE_LOCAL_HZ = 600, TONE_REMOTE_HZ = 700, RAMP_S = 0.005;
// The Vibration API only takes durations, and a cancel issued right after a
// start can be lost (the buzz is started asynchronously by the OS), so "hold"
// is a train of short pulses: a lost cancel overshoots by one pulse, not seconds.
const VIBRATE_PULSE_MS = 100, VIBRATE_REISSUE_MS = 80;

export const ac = new AudioContext();

// Always-running oscillator gated by gain, so on/off is click-free.
function tone(hz) {
  const osc = new OscillatorNode(ac, { frequency: hz });
  const gain = new GainNode(ac, { gain: 0 });
  osc.connect(gain).connect(ac.destination);
  osc.start();
  return on => gain.gain.setTargetAtTime(on && settings.sound ? 1 : 0, ac.currentTime, RAMP_S);
}

// The remote pitch differs so you can tell who is keying.
export const localTone = tone(TONE_LOCAL_HZ);
export const remoteTone = tone(TONE_REMOTE_HZ);

let vibTimer = null;
export function vibrate(on) {
  clearInterval(vibTimer);
  vibTimer = null;
  if (!on) { navigator.vibrate(0); return; }
  navigator.vibrate(VIBRATE_PULSE_MS);
  vibTimer = setInterval(() => navigator.vibrate(VIBRATE_PULSE_MS), VIBRATE_REISSUE_MS);
}
