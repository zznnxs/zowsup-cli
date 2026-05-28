import { useEffect } from 'react';
import { Badge } from 'antd';

import { useEventStream } from '../store/events';

// EventStreamIndicator opens the /api/events websocket on mount and shows
// a coloured dot in the header. Slot it once near the top of the layout.
export function EventStreamIndicator() {
  const status = useEventStream((s) => s.status);
  const connect = useEventStream((s) => s.connect);
  const disconnect = useEventStream((s) => s.disconnect);

  useEffect(() => {
    connect();
    return () => disconnect();
  }, [connect, disconnect]);

  const colour = status === 'open' ? '#25D366' : status === 'connecting' ? '#faad14' : '#ff4d4f';
  const label =
    status === 'open' ? 'Event 流已连接' : status === 'connecting' ? '正在连接事件流…' : '事件流已断开';
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, color: '#aaa', fontSize: 13 }}>
      <Badge color={colour} />
      {label}
    </span>
  );
}
