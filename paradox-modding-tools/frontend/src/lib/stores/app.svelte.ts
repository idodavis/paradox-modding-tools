import { writable, derived, get } from 'svelte/store';
import { SaveSettings, GetSettings } from '@services/settingsservice';

export const game = writable<'CK3' | 'EU5'>('CK3');
export const currentPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const lastPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const helpOpen = writable<boolean>(false);

export const appSettings = writable<Record<string, string>>({});

export const gameInstallPath = derived(
  [game, appSettings],
  ([$g, $s]) => $g === 'CK3' ? ($s['ck3.install_path'] ?? '') : ($s['eu5.install_path'] ?? '')
);

export const appConstants = derived(appSettings, ($s) => ({
  ck3: { scriptRootFolder: $s['ck3.ck3_scriptRootFolder'], wikiUrl: $s['ck3.ck3_wikiUrl'] },
  eu5: { scriptRootFolder: $s['eu5.eu5_scriptRootFolder'], wikiUrl: $s['eu5.eu5_wikiUrl'] },
}));

export function gotoPage(page: 'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory') {
  currentPage.update((current) => {
    helpOpen.set(false);
    if (current !== page) {
      lastPage.set(current);
    }
    return page;
  });
}

export function goBack() {
  currentPage.update((current) => get(lastPage));
}

export async function loadSettings() {
  const settings = await GetSettings()
  const filteredSettings = Object.fromEntries(
    Object.entries(settings).filter(([, value]) => value !== undefined)
  ) as Record<string, string>;
  appSettings.set(filteredSettings)
}

export async function saveSettings() {
  await SaveSettings(get(appSettings));
}
