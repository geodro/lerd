export type DiffOp = ' ' | '-' | '+';

export interface DiffLine {
  op: DiffOp;
  line: string;
}

/**
 * Classic LCS line diff. Returns a flat sequence of context, removed, and
 * added lines reconstructed by walking the LCS table. Adequate for short
 * files like .env (typically &lt;100 lines).
 */
export function diffLines(a: string, b: string): DiffLine[] {
  const aLines = a === '' ? [] : a.split('\n');
  const bLines = b === '' ? [] : b.split('\n');
  const m = aLines.length;
  const n = bLines.length;

  if (m === 0 && n === 0) return [];
  if (m === 0) return bLines.map((line) => ({ op: '+', line }));
  if (n === 0) return aLines.map((line) => ({ op: '-', line }));

  // dp[i][j] = LCS length of aLines[i..] vs bLines[j..]
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i][j] = aLines[i] === bLines[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (aLines[i] === bLines[j]) {
      out.push({ op: ' ', line: aLines[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ op: '-', line: aLines[i] });
      i++;
    } else {
      out.push({ op: '+', line: bLines[j] });
      j++;
    }
  }
  while (i < m) out.push({ op: '-', line: aLines[i++] });
  while (j < n) out.push({ op: '+', line: bLines[j++] });
  return out;
}
