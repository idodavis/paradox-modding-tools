import { writable, get } from 'svelte/store';
import { SaveSettings, GetSettings } from '@services/settingsservice';
import { GetAppConstants } from '@services/constantsservice';
import type { AppConstants } from '@services/models';

export const game = writable<'CK3' | 'EU5'>('CK3');
export const currentPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const lastPage = writable<'hub' | 'settings' | 'compare-tool' | 'modding-docs' | 'merge-tool' | 'inventory'>('hub');
export const gameInstallPathCk3 = writable<string>('');
export const gameInstallPathEu5 = writable<string>('');
export const appConstants = writable<AppConstants>({
  ck3: {
    steamAppId: '',
    wikiUrl: '',
    docFileName: '',
    scriptRootFolder: '',
  },
  eu5: {
    steamAppId: '',
    wikiUrl: '',
    docFileName: '',
    scriptRootFolder: '',
  },
});

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
}

export async function saveSettings() {
  await SaveSettings({
    gameInstallPathCk3: get(gameInstallPathCk3),
    gameInstallPathEu5: get(gameInstallPathEu5),
  });
}

export async function loadAppConstants() {
  const constants = await GetAppConstants();
  appConstants.set(constants);
}
