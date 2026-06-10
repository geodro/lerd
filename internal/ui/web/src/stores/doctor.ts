import { apiJson } from '$lib/api';

// Mirrors ui.DoctorCheck on the Go side. `fix`, when set, names a command from
// the site's command set (loadCommands) that resolves the finding.
export interface DoctorCheck {
  name: string;
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
  const data = await apiJson<DoctorReport>(path + q);
  return {
    checks: Array.isArray(data.checks) ? data.checks : [],
    failures: data.failures ?? 0,
    warnings: data.warnings ?? 0
  };
}
