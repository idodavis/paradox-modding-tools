import { writable, get } from 'svelte/store';
import { SaveSettings, GetSettings } from '../../../bindings/paradox-modding-tools/services/settingsservice';

export const game = writable<'CK3' | 'EU5'>('CK3');
export const currentPage = writable<'hub' | 'compare-tool' | 'settings'>('hub');
export const lastPage = writable<'hub' | 'compare-tool' | 'settings'>('hub');
export const gameInstallPathCk3 = writable<string>('');
export const gameInstallPathEu5 = writable<string>('');

export function gotoPage(page: 'hub' | 'compare-tool' | 'settings') {
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

