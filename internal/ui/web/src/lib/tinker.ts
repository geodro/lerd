// Helpers for parsing the tinker output stream. The backend frames each
// top-level statement's output as `\x1e<line>\x1f<output>`: the record
// separator (\x1e) splits blocks, and the leading `<line>\x1f` field carries
// the editor line that produced the block so the UI can label it "Line N".

// ASCII unit separator (0x1f) ends the line field. Requiring it as the
// terminator means plain numeric output like `42` is never mistaken for a
// marker.
const UNIT_SEPARATOR = String.fromCharCode(0x1f);
const BLOCK_MARKER_RE = new RegExp('^(\\d+)' + UNIT_SEPARATOR);

// peelBlockMarker pulls the optional `<line>\x1f` marker off the front of a
// chunk, returning the source line (when present) and the remaining output.
// Chunks without a marker (legacy single blocks, stray noise) pass through
// with no line.
export function peelBlockMarker(chunk: string): { line?: number; rest: string } {
  const m = chunk.match(BLOCK_MARKER_RE);
  if (!m) return { rest: chunk };
  return { line: parseInt(m[1], 10), rest: chunk.slice(m[0].length) };
}

// ASCII STX (0x02) prefixes a query block's payload (`\x1e<line>\x1f\x02<sql>`)
// so a captured SQL query is distinguishable from statement output.
const QUERY_MARKER = String.fromCharCode(0x02);

// parseBlock peels the line marker and classifies the block as a captured SQL
// query (STX-prefixed) or ordinary statement output, returning the remaining
// body in each case.
export function parseBlock(chunk: string): { line?: number; kind: 'query' | 'output'; body: string } {
  const { line, rest } = peelBlockMarker(chunk);
  if (rest.startsWith(QUERY_MARKER)) {
    return { line, kind: 'query', body: rest.slice(QUERY_MARKER.length) };
  }
  return { line, kind: 'output', body: rest };
}
