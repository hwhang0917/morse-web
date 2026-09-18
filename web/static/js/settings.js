const STORAGE_KEY = 'settings';
// Flash stays off by default: a full-screen strobe is a seizure risk.
const DEFAULTS = { sound: true, vibrate: false, flash: false, human: false, translate: false };

function load() {
  try {
    return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(STORAGE_KEY)) };
  } catch {
    return { ...DEFAULTS };
  }
}

function save() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
  } catch {
    // Storage can be unavailable in private windows; the in-memory copy still works.
  }
}

export const settings = load();

export function bindSettings(onChange) {
  for (const checkbox of document.querySelectorAll('[data-setting]')) {
    const key = checkbox.dataset.setting;
    checkbox.checked = settings[key];
    checkbox.addEventListener('change', () => {
      settings[key] = checkbox.checked;
      save();
      onChange(key, checkbox.checked);
    });
  }
}
