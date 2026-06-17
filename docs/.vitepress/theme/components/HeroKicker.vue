<template>
  <div class="hero-kicker">
    <span class="hk-dot"></span>
    ROOTLESS PODMAN · NO DOCKER · NO SUDO
  </div>
</template>

<style scoped>
.hero-kicker {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12.5px;
  letter-spacing: 0.14em;
  color: var(--vp-c-text-2);
  margin-bottom: 18px;
}

.hk-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--vp-c-brand-1);
  box-shadow: 0 0 12px var(--vp-c-brand-1);
  animation: hk-blink 1.6s steps(1) infinite;
}

@keyframes hk-blink {
  50% { opacity: 0.25; }
}

@media (max-width: 960px) {
  .hero-kicker { justify-content: center; }
}
</style>
