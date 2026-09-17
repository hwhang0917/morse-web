// Settings persisted in localStorage.

// Flash is off by default: a full-screen strobe is a seizure risk.
const DEFAULT_SETTINGS = { sound: true, vibrate: false, flash: false, human: false };
const STORAGE_KEY = 'settings';

function load() {
  try { return { ...DEFAULT_SETTINGS, ...JSON.parse(localStorage.getItem(STORAGE_KEY)) }; }
  catch { return { ...DEFAULT_SETTINGS }; }
}

function save() {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings)); } catch {}
}

export const settings = load();

// Binds every [data-setting] checkbox to its key. onChange(key, value) fires
// after each change so the caller can stop whatever a disabled setting drives.
export function bindSettings(onChange) {
  for (const box of document.querySelectorAll('[data-setting]')) {
    const key = box.dataset.setting;
    box.checked = settings[key];
    box.addEventListener('change', () => {
      settings[key] = box.checked;
      save();
      onChange(key, box.checked);
    });
  }
}
