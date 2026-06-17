<template>
  <section class="shipit">
    <div class="shipit-inner">
      <div class="shipit-copy">
        <div class="si-kicker"><span class="si-dot"></span> GET RUNNING</div>
        <h2>Ship it to your<br>machine.</h2>
        <p>One curl on Linux, one brew on macOS. Then <code>lerd link</code> any project and it's live at
        <code>project.test</code> with HTTPS, no Docker daemon and no sudo.</p>
      </div>

      <div class="term">
        <div class="term-bar">
          <i></i><i></i><i></i>
          <span>bash</span>
        </div>
        <div class="term-body">
          <div class="c"># Linux</div>
          <div><span class="p">$</span> curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash</div>
          <div class="c"># macOS</div>
          <div><span class="p">$</span> brew install geodro/lerd/lerd &amp;&amp; lerd install</div>
          <div class="c"># then, in any project</div>
          <div><span class="p">$</span> lerd link <span class="o"># → https://project.test ✓</span></div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.shipit {
  max-width: 1152px;
  margin: 8px auto 0;
  padding: 64px 24px 16px;
}

.shipit-inner {
  display: grid;
  grid-template-columns: 1fr 1.15fr;
  gap: 44px;
  align-items: center;
  border-top: 1px solid var(--vp-c-divider);
  padding-top: 56px;
}

.si-kicker {
  display: flex;
  align-items: center;
  gap: 9px;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12px;
  letter-spacing: 0.16em;
  color: var(--vp-c-text-2);
}

.si-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--vp-c-brand-1);
  box-shadow: 0 0 10px var(--vp-c-brand-1);
}

.shipit-copy h2 {
  font-family: 'Archivo', var(--vp-font-family-base);
  font-weight: 900;
  font-stretch: 125%;
  text-transform: uppercase;
  letter-spacing: -0.02em;
  line-height: 0.98;
  font-size: clamp(34px, 5vw, 52px);
  margin: 16px 0 0;
}

.shipit-copy h2 br { display: block; }

.shipit-copy p {
  margin-top: 18px;
  color: var(--vp-c-text-2);
  font-size: 15px;
  line-height: 1.7;
  max-width: 440px;
}

.shipit-copy code {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12.5px;
  color: var(--vp-c-brand-1);
  background: var(--vp-c-brand-soft);
  border-radius: 5px;
  padding: 1px 6px;
}

.term {
  background: #0c0c0e;
  border: 1px solid #232329;
  border-radius: 14px;
  overflow: hidden;
  box-shadow: 0 28px 64px rgba(0, 0, 0, 0.45);
}

.term-bar {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 12px 16px;
  border-bottom: 1px solid #232329;
  background: #131316;
}

.term-bar i {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: #2e2e36;
}

.term-bar i:first-child {
  background: var(--vp-c-brand-1);
}

.term-bar span {
  margin-left: auto;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 11px;
  color: #67645c;
}

.term-body {
  padding: 20px 22px;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12.5px;
  line-height: 2.05;
  color: #c9c5bd;
  overflow-x: auto;
}

.term-body div {
  white-space: pre;
}

.term-body .c {
  color: #67645c;
}

.term-body .p {
  color: var(--vp-c-brand-1);
  margin-right: 6px;
}

.term-body .o {
  color: #3dd68c;
}

@media (max-width: 860px) {
  .shipit-inner {
    grid-template-columns: 1fr;
    gap: 32px;
  }
  .shipit { padding-top: 40px; }
}
</style>
