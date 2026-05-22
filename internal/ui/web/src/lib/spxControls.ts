// Helpers for tailoring the embedded SPX profiler UI. SPX's web UI is
// upstream php-spx, reverse-proxied same-origin under /_spx/; lerd reaches
// into that iframe to collapse the control panel's Configuration form, pad
// the control panel page, and surface freshly captured reports without a
// manual refresh. The selectors and the metadata URL below are the only
// points of coupling to SPX's internals.

// SPX_METADATA_URL is the endpoint the SPX control panel page itself uses to
// list captured reports. Polling it lets lerd notice new captures.
const SPX_METADATA_URL = '/_spx/?SPX_UI_URI=/data/reports/metadata';

// CONFIG_FORM is the Configuration fieldset wrapper on the control panel page.
const CONFIG_FORM = '#config';
const PAD_STYLE_ID = 'lerd-spx-pad';

// True for the SPX single-report analysis screen, false for the control panel.
export function isSpxReportView(href: string): boolean {
  return href.includes('SPX_UI_URI=/report.html');
}

// setSpxConfigHidden shows or hides the Configuration form on the SPX control
// panel page, which otherwise pushes the report list far down the page.
// Returns whether the form was found.
export function setSpxConfigHidden(doc: Document, hidden: boolean): boolean {
  const form = doc.querySelector(CONFIG_FORM);
  // The SPX page runs in the iframe's own realm, so test against that realm's
  // HTMLElement; a plain `instanceof HTMLElement` would always be false here.
  const HtmlEl = doc.defaultView?.HTMLElement ?? HTMLElement;
  if (!(form instanceof HtmlEl)) return false;
  form.style.display = hidden ? 'none' : '';
  return true;
}

// padSpxControlPanel insets the SPX control panel page, whose content
// otherwise sits flush against the iframe edges. Idempotent.
export function padSpxControlPanel(doc: Document): void {
  if (doc.getElementById(PAD_STYLE_ID)) return;
  const style = doc.createElement('style');
  style.id = PAD_STYLE_ID;
  style.textContent = 'body{padding:20px;box-sizing:border-box}';
  doc.head.appendChild(style);
}

// fetchSpxReportCount returns how many SPX reports currently exist, read from
// the same metadata endpoint the control panel page uses. Returns null on any
// failure so callers can simply skip that poll.
export async function fetchSpxReportCount(): Promise<number | null> {
  try {
    const res = await fetch(SPX_METADATA_URL, { credentials: 'same-origin' });
    if (!res.ok) return null;
    const data = (await res.json()) as { results?: unknown };
    return Array.isArray(data.results) ? data.results.length : null;
  } catch {
    return null;
  }
}
