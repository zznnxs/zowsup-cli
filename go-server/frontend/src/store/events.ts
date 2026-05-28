import { create } from 'zustand';

export interface BusEvent {
  topic: string;
  account_id: number;
  payload: unknown;
  at: number;
}

interface EventStreamState {
  status: 'idle' | 'connecting' | 'open' | 'closed';
  events: BusEvent[];
  connect: () => void;
  disconnect: () => void;
}

const MAX_EVENTS = 200;

// useEventStream wraps the /api/events websocket. It is intentionally
// lightweight: a singleton WS, last N events kept for the indicator badge,
// and idempotent connect/disconnect.
export const useEventStream = create<EventStreamState>((set, get) => {
  let ws: WebSocket | null = null;
  let retry: number | null = null;

  const open = () => {
    if (ws) return;
    set({ status: 'connecting' });
    const url =
      (location.protocol === 'https:' ? 'wss://' : 'ws://') +
      location.host +
      '/api/events';
    const sock = new WebSocket(url);
    ws = sock;
    sock.onopen = () => set({ status: 'open' });
    sock.onclose = () => {
      ws = null;
      set({ status: 'closed' });
      // exponential-ish backoff to 30s.
      retry = window.setTimeout(() => get().connect(), 3000);
    };
    sock.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data) as BusEvent;
        set((s) => ({ events: [ev, ...s.events].slice(0, MAX_EVENTS) }));
      } catch {
        /* ignore */
      }
    };
  };

  return {
    status: 'idle',
    events: [],
    connect: () => {
      if (retry) {
        window.clearTimeout(retry);
        retry = null;
      }
      open();
    },
    disconnect: () => {
      if (retry) {
        window.clearTimeout(retry);
        retry = null;
      }
      ws?.close();
      ws = null;
      set({ status: 'closed' });
    },
  };
});
