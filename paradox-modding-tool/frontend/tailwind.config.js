/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        'dark-bg': '#0f1419',
        'dark-panel': '#1a1f2e',
        'dark-input': '#151922',
        'dark-border': '#2a3441',
        'btn-primary': '#2563eb',
        'btn-primary-hover': '#3b82f6',
        'accent-success': '#22c55e',
        'diff-add': 'rgba(16, 185, 129, 0.2)',
        'diff-remove': 'rgba(239, 68, 68, 0.2)',
        'diff-context': 'transparent',
        'diff-header': 'rgba(148, 163, 184, 0.1)',
      },
      backdropBlur: {
        xs: '2px',
      },
      boxShadow: {
        'material': '0 2px 8px rgba(0, 0, 0, 0.3), 0 1px 3px rgba(0, 0, 0, 0.2)',
        'material-lg': '0 4px 16px rgba(0, 0, 0, 0.4), 0 2px 6px rgba(0, 0, 0, 0.3)',
      },
    },
  },
  plugins: [],
}
