import { writable, derived, get } from 'svelte/store';
import { SaveSettings, GetSettings } from '@services/settingsservice';
import type { AppSettings } from '@services/models';

export const game = writable<'CK3' | 'EU5'>('CK3');
export const currentPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const lastPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const gameInstallPathCk3 = writable<string>('');
export const gameInstallPathEu5 = writable<string>('');

export const gameInstallPath = derived(
  [game, gameInstallPathCk3, gameInstallPathEu5],
  ([$g, $ck3, $eu5]) => ($g === 'CK3' ? $ck3 : $eu5)
);

export const appSettings = writable<AppSettings>({
  gameInstallPathCk3: '',
  gameInstallPathEu5: '',
  ck3_steamAppId: '',
  ck3_wikiUrl: '',
  ck3_scriptRootFolder: '',
  ck3_docFileName: '',
  eu5_steamAppId: '',
  eu5_wikiUrl: '',
  eu5_scriptRootFolder: '',
  eu5_docFileName: '',
});

export const appConstants = derived(appSettings, ($s) => ({
  ck3: { scriptRootFolder: $s.ck3_scriptRootFolder, wikiUrl: $s.ck3_wikiUrl },
  eu5: { scriptRootFolder: $s.eu5_scriptRootFolder, wikiUrl: $s.eu5_wikiUrl },
}));

export function gotoPage(page: 'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory') {
  currentPage.update((current) => {
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
  gameInstallPathCk3.set(settings.gameInstallPathCk3 ?? '')
  gameInstallPathEu5.set(settings.gameInstallPathEu5 ?? '')
  appSettings.set(settings)
}

export async function saveSettings() {
  await SaveSettings({
    gameInstallPathCk3: get(gameInstallPathCk3),
    gameInstallPathEu5: get(gameInstallPathEu5),
    ck3_steamAppId: get(appSettings).ck3_steamAppId,
    ck3_wikiUrl: get(appSettings).ck3_wikiUrl,
    ck3_scriptRootFolder: get(appSettings).ck3_scriptRootFolder,
    ck3_docFileName: get(appSettings).ck3_docFileName,
    eu5_steamAppId: get(appSettings).eu5_steamAppId,
    eu5_wikiUrl: get(appSettings).eu5_wikiUrl,
    eu5_scriptRootFolder: get(appSettings).eu5_scriptRootFolder,
    eu5_docFileName: get(appSettings).eu5_docFileName,
  });
}
