import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import HeroKicker from './components/HeroKicker.vue'
import DigestBanner from './components/DigestBanner.vue'
import ShipIt from './components/ShipIt.vue'
import './style.css'

export default {
  extends: DefaultTheme,
  Layout() {
    return h(DefaultTheme.Layout, null, {
      'home-hero-info-before': () => h(HeroKicker),
      'home-hero-after': () => h(DigestBanner),
      'home-features-after': () => h(ShipIt),
    })
  },
}
