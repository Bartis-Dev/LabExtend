import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./app/**/*.{ts,tsx}', './components/**/*.{ts,tsx}', './lib/**/*.{ts,tsx}'],
  theme: {
    extend: {
      // 1920px+ monitors get their own step so grids can add a column there.
      screens: {
        '3xl': '1920px',
      },
      colors: {
        // Neutral grayscale + a single accent (per the user's design taste:
        // premium minimal, UniFi/Apple-leaning).
        accent: {
          DEFAULT: '#2563EB',
          hover: '#1D4ED8',
          active: '#1E40AF',
          fg: '#FFFFFF',
        },
      },
      // Small text sizes in rem so they follow the root scale in globals.css.
      // These replace the old text-[10px] / text-[11px] literals.
      fontSize: {
        '3xs': ['0.625rem', { lineHeight: '0.875rem' }],
        '2xs': ['0.6875rem', { lineHeight: '1rem' }],
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
};

export default config;
