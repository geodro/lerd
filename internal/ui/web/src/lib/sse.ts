// readSSE reads a Response body as Server-Sent Events, invoking onFrame once per
// complete frame with the event name (empty string when none was given) and the
// `data:` payload. lerd's streams emit one `data:` line per frame, so the event
// name is consumed and reset after each data line. Throws if there is no body.
export async function readSSE(
  res: Response,
  onFrame: (event: string, data: string) => void
): Promise<void> {
  if (!res.body) throw new Error('no response body');
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let eventType = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const lines = buf.split('\n');
    buf = lines.pop() ?? '';
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7);
      } else if (line.startsWith('data: ')) {
        onFrame(eventType, line.slice(6));
        eventType = '';
      } else if (line === '') {
        eventType = '';
      }
    }
  }
}
