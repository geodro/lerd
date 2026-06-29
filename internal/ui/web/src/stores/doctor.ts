import { apiJson } from '$lib/api';

// Mirrors ui.DoctorCheck on the Go side. `fix`, when set, names a command from
// the site's command set (loadCommands) that resolves the finding.
export interface DoctorCheck {
  name: string;
  label?: string;
  status: 'ok' | 'warn' | 'fail' | 'unknown';
  detail?: string;
  fix?: string;
}

export interface DoctorReport {
  checks: DoctorCheck[];
  failures: number;
  warnings: number;
}

export async function loadDoctor(domain: string, branch = ''): Promise<DoctorReport> {
  const path = `/api/sites/${encodeURIComponent(domain)}/doctor`;
  const q = branch ? `?branch=${encodeURIComponent(branch)}` : '';
  const data = await apiJson<DoctorReport & { error?: string }>(path + q);
  // The route returns 200 with an { error } body for refusals (unknown
  // worktree branch, site not found), so surface it instead of rendering an
  // empty "all clear".
  if (data.error) throw new Error(data.error);
  return {
    checks: Array.isArray(data.checks) ? data.checks : [],
    failures: data.failures ?? 0,
    warnings: data.warnings ?? 0
  };
}
