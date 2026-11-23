import type { BadgeVariant } from '@prusa/connect-ui/primitives';

/**
 * The badge each Prusa Connect printer state is drawn with, the tones the
 * core's printer listing uses. A state the map does not name is drawn quiet.
 */
export const STATE_VARIANTS: Readonly<Record<string, BadgeVariant>> = {
  READY: 'stateReady',
  IDLE: 'stateIdle',
  FINISHED: 'stateFinished',
  STOPPED: 'stateStopped',
  PRINTING: 'stateActive',
  PREPARED: 'stateActive',
  BUSY: 'stateBusy',
  PAUSED: 'statePaused',
  MANIPULATING: 'stateActive',
  ATTENTION: 'stateAttention',
  ERROR: 'stateFault',
  OFFLINE: 'stateOffline',
  UNKNOWN: 'stateQuiet',
};
