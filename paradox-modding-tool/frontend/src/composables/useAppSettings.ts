import { ref, computed, onMounted } from 'vue'
import { GetSettings, SaveSettings } from '../../bindings/paradox-modding-tool/settingsservice.js'
import type { AppSettings } from '../../bindings/paradox-modding-tool/models.js'

/** Default Steam install paths (hints/placeholders). Windows; adjust for macOS/Linux if needed. */
export const DEFAULT_STEAM_PATH_CK3 =
  'C:\\Program Files (x86)\\Steam\\steamapps\\common\\Crusader Kings III'
export const DEFAULT_STEAM_PATH_EU5 =
  'C:\\Program Files (x86)\\Steam\\steamapps\\common\\Europa Universalis V'

const game = ref<string>('ck3')
const gameInstallPathCk3 = ref<string>('')
const gameInstallPathEu5 = ref<string>('')

/** Install path for the currently selected game. */
const gameInstallPath = computed(() =>
  game.value === 'eu5' ? gameInstallPathEu5.value : gameInstallPathCk3.value
)

let loading = false

async function loadSettings() {
  if (loading) return
  loading = true
  try {
    const settings = await GetSettings()
    game.value = settings.game || 'ck3'
    gameInstallPathCk3.value = settings.gameInstallPathCk3 ?? ''
    gameInstallPathEu5.value = settings.gameInstallPathEu5 ?? ''
  } finally {
    loading = false
  }
}

async function persist() {
  const settings: AppSettings = {
    game: game.value,
    gameInstallPathCk3: gameInstallPathCk3.value,
    gameInstallPathEu5: gameInstallPathEu5.value,
    patchNotesFeedUrlCk3: '',
    patchNotesFeedUrlEu5: ''
  }
  await SaveSettings(settings)
}

/** Set current game and persist. */
async function setGame(value: string) {
  if (value !== 'ck3' && value !== 'eu5') return
  game.value = value
  await persist()
}

/** Set CK3 install path and persist. */
async function setGameInstallPathCk3(path: string) {
  gameInstallPathCk3.value = path
  await persist()
}

/** Set EU5 install path and persist. */
async function setGameInstallPathEu5(path: string) {
  gameInstallPathEu5.value = path
  await persist()
}

/** Set install path for the current game and persist. */
async function setGameInstallPath(path: string) {
  if (game.value === 'eu5') {
    gameInstallPathEu5.value = path
  } else {
    gameInstallPathCk3.value = path
  }
  await persist()
}

export function useAppSettings() {
  onMounted(loadSettings)
  return {
    game,
    gameInstallPathCk3,
    gameInstallPathEu5,
    gameInstallPath,
    loadSettings,
    setGame,
    setGameInstallPathCk3,
    setGameInstallPathEu5,
    setGameInstallPath,
    persist
  }
}
