import type { StatusTone } from './types';

// The dot, one static string per tone (svelte-5.md). The `edge` step is the
// one the theme measured for a small filled shape; the `text` step is for
// words and the plain step for a panel behind them.
export const dots: Record<StatusTone, string> = {
  active: 'bg-state-active-edge',
  done: 'bg-state-done-edge',
  idle: 'bg-state-idle-edge',
  attention: 'bg-state-attention-edge',
  fault: 'bg-state-fault-edge',
  quiet: 'bg-state-quiet-edge',
};

// The start edge of an `edge` card, one static string per tone.
export const edges: Record<StatusTone, string> = {
  active: 'border-s-state-active-edge',
  done: 'border-s-state-done-edge',
  idle: 'border-s-state-idle-edge',
  attention: 'border-s-state-attention-edge',
  fault: 'border-s-state-fault-edge',
  quiet: 'border-s-state-quiet-edge',
};
