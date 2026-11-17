import type { ClassValue } from 'svelte/elements';

export type ConnectionState = 'online' | 'offline';

export type ConnectionStatusPresentation = 'badge' | 'dot';

export interface ConnectionStatusProps {
  state: ConnectionState;
  presentation?: ConnectionStatusPresentation;
  class?: ClassValue | undefined;
}
