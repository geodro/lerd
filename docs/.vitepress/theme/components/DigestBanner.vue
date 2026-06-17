<script setup>
import { withBase } from 'vitepress'
</script>

<template>
  <div class="digest-banner-wrap">
    <a class="digest-banner" :href="withBase('/digest/v1.25.0.html')" target="_blank" rel="noopener">
      <span class="db-pill">NEW</span>
      <span class="db-body">
        <span class="db-title">v1.25.0 dev digest</span>
        <span class="db-sub">Idle-suspend, bun runtime, custom PHP images, FrankenPHP parity, and a broader dashboard.</span>
      </span>
      <span class="db-cta">Read the digest&nbsp;→</span>
    </a>
  </div>
</template>

<style scoped>
.digest-banner-wrap {
  max-width: 1152px;
  margin: 24px auto 8px;
  padding: 0 24px;
}

@media (min-width: 640px) {
  .digest-banner-wrap {
    margin: 32px auto 16px;
  }
}

@media (min-width: 960px) {
  .digest-banner-wrap {
    margin: 40px auto 24px;
  }
}

.digest-banner {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 16px 20px;
  border-radius: 14px;
  text-decoration: none;
  color: var(--vp-c-text-1);
  border: 1px solid var(--vp-c-divider);
  background:
    radial-gradient(120% 140% at 0% 0%, rgba(255, 45, 32, 0.10), transparent 55%),
    var(--vp-c-bg-soft);
  transition: border-color 0.25s ease, transform 0.25s ease, box-shadow 0.25s ease;
}

.digest-banner:hover {
  border-color: var(--vp-c-brand-1);
  transform: translateY(-2px);
  box-shadow: 0 14px 36px rgba(255, 45, 32, 0.12);
}

.db-pill {
  flex: 0 0 auto;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.12em;
  color: #fff;
  background: var(--vp-c-brand-1);
  border-radius: 999px;
  padding: 4px 11px;
  box-shadow: 0 0 18px rgba(255, 45, 32, 0.35);
}

.db-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.db-title {
  font-weight: 700;
  font-size: 15px;
}

.db-sub {
  font-size: 13px;
  color: var(--vp-c-text-2);
  line-height: 1.5;
}

.db-cta {
  flex: 0 0 auto;
  margin-left: auto;
  font-weight: 600;
  font-size: 13px;
  color: var(--vp-c-brand-1);
  white-space: nowrap;
}

@media (max-width: 640px) {
  .digest-banner {
    flex-wrap: wrap;
    gap: 10px 14px;
  }
  .db-cta {
    margin-left: 0;
  }
}
</style>
