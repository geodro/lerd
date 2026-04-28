import { writable } from 'svelte/store';
import { wsUrl } from './api';

export interface WsMessage {
  type: string;
  sites?: unknown;
  services?: unknown;
  status?: unknown;
}

export const wsConnected = writable<boolean>(false);
export const wsMessage = writable<WsMessage | null>(null);

let socket: WebSocket | null = null;
let backoff = 1000;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

export function connectWs() {
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
    return;
  }
  try {
    const ws = new WebSocket(wsUrl('/api/ws'));
    socket = ws;
    ws.addEventListener('open', () => {
      backoff = 1000;
      wsConnected.set(true);
      try {
        ws.send(JSON.stringify({ type: 'visibility', visible: !document.hidden }));
      } catch {
        /* non-fatal */
      }
    });
    ws.addEventListener('message', (e) => {
      try {
        const msg = JSON.parse(e.data) as WsMessage;
        wsMessage.set(msg);
      } catch {
        /* ignore bad frames */
      }
    });
    ws.addEventListener('close', () => {
      socket = null;
      wsConnected.set(false);
      const delay = Math.min(30000, backoff);
      backoff = Math.min(30000, backoff * 2);
      reconnectTimer = setTimeout(connectWs, delay);
    });
    ws.addEventListener('error', () => {
      /* onclose fires next and handles reconnect */
    });
  } catch {
    reconnectTimer = setTimeout(connectWs, 1000);
  }
}

export function disconnectWs() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (socket) {
    socket.close();
    socket = null;
  }
}
