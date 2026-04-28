export default {
  content: ['./index.html', './src/**/*.{svelte,ts,js}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        lerd: {
          red: '#FF2D20',
          redhov: '#e02419',
          bg: '#0d0d0d',
          card: '#161616',
          border: '#262626',
          muted: '#404040'
        }
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif']
      }
    }
  },
  plugins: []
};
