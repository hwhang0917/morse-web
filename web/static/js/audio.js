import { settings } from './settings.js';

const LOCAL_TONE_HZ = 600;
const REMOTE_TONE_HZ = 700;
const GAIN_RAMP_S = 0.005;
// The OS starts a vibration asynchronously, so a cancel issued right after a
// start can be lost. Holding is therefore a train of short pulses: a lost
// cancel overshoots by one pulse instead of seconds.
const VIBRATE_PULSE_MS = 100;
const VIBRATE_REISSUE_MS = 80;

export const audioContext = new AudioContext();

function createTone(frequency) {
  const oscillator = new OscillatorNode(audioContext, { frequency });
  const gain = new GainNode(audioContext, { gain: 0 });
  oscillator.connect(gain).connect(audioContext.destination);
  oscillator.start();

  return on => {
    const level = on && settings.sound ? 1 : 0;
    gain.gain.setTargetAtTime(level, audioContext.currentTime, GAIN_RAMP_S);
  };
}

export const localTone = createTone(LOCAL_TONE_HZ);
export const remoteTone = createTone(REMOTE_TONE_HZ);

let vibrateTimer = null;

export function vibrate(on) {
  clearInterval(vibrateTimer);
  vibrateTimer = null;
  if (!on) {
    navigator.vibrate(0);
    return;
  }
  navigator.vibrate(VIBRATE_PULSE_MS);
  vibrateTimer = setInterval(() => navigator.vibrate(VIBRATE_PULSE_MS), VIBRATE_REISSUE_MS);
}
