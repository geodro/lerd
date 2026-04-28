import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';
import { initTheme } from '$stores/theme';

initTheme();

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js')
      .then((reg) => {
        console.info('[lerd] SW registered, scope=', reg.scope);
      })
      .catch((err) => {
        console.warn('[lerd] SW registration failed:', err);
      });
  });
} else {
  console.warn('[lerd] serviceWorker unavailable (insecure context? private mode?)');
}

const target = document.getElementById('app');
if (!target) throw new Error('missing #app root');

export default mount(App, { target });
