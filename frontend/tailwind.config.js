/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ag: {
          bg: '#0b0d12',
          sidebar: '#10131a',
          panel: '#141822',
          elevated: '#1a202e',
          hover: '#21283a',
          border: '#23293a',
          borderStrong: '#303952',
          accent: '#6366f1',
          accentHover: '#7c7ff8',
          cyan: '#38bdf8',
          text: '#e6eaf5',
          muted: '#8c96b0',
          subtle: '#5f6982',
        },
      },
      fontFamily: {
        sans: ['"Segoe UI Variable"', '"Segoe UI"', 'Inter', '"PingFang SC"', '"Microsoft YaHei"', 'sans-serif'],
        mono: ['"Cascadia Mono"', '"JetBrains Mono"', 'Consolas', 'monospace'],
      },
      boxShadow: {
        ag: '0 12px 36px rgba(0, 0, 0, 0.55)',
        glow: '0 0 0 1px rgba(99, 102, 241, 0.35), 0 8px 28px rgba(99, 102, 241, 0.16)',
      },
    },
  },
  plugins: [],
}
