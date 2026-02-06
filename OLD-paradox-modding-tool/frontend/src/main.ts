// @ts-ignore
import '@primeuix/styles'; // When styles change, the app will hot reload.

import { createApp } from 'vue'
import PrimeVue from 'primevue/config'
import Aura from '@primeuix/themes/aura'
import { definePreset } from '@primeuix/themes';
import App from './App.vue'
import './style.css'

const app = createApp(App)

export const MyPreset = definePreset(Aura, {
  semantic: {
    primary: {
      50: '{indigo.50}',
      100: '{indigo.100}',
      200: '{indigo.200}',
      300: '{indigo.300}',
      400: '{indigo.400}',
      500: '{indigo.500}',
      600: '{indigo.600}',
      700: '{indigo.700}',
      800: '{indigo.800}',
      900: '{indigo.900}',
      950: '{indigo.950}'
    },
    colorScheme: {
        dark: {
          surface: {
            50: '{slate.50}',
            100: '{slate.100}',
            200: '{slate.200}',
            300: '{slate.300}',
            400: '{slate.400}',
            500: '{slate.500}',
            600: '{slate.600}',
            700: '{slate.700}',
            800: '{slate.800}',
            900: '{slate.900}',
            950: '{slate.950}'
          },
          primary: {
            color: '{primary.600}',
            inverseColor: '{primary.950}',
            hoverColor: '{primary.400}',
            activeColor: '{primary.200}'
          }
        }
    }
  }
});

app.use(PrimeVue, {
  theme: {
    preset: MyPreset,
    options: {
      darkModeSelector: '.my-dark-theme'
    }
  },
  pt: {
    Panel: {
      contentContainer: { class: 'min-w-0 overflow-hidden' },
      contentWrapper: { class: 'min-w-0 overflow-hidden flex flex-col' },
      content: { class: 'min-w-0 flex flex-col flex-1 overflow-hidden' }
    },
    Tabs: { root: { class: 'flex-1 min-h-0 flex flex-col' } },
    TabPanels: { root: { class: 'flex-1 min-h-0 flex flex-col overflow-hidden' } },
    TabPanel: { root: { class: 'flex flex-col flex-1 min-h-0 overflow-hidden' } }
  }
})

app.mount('#app')
