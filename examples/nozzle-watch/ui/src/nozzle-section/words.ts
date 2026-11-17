// The word and the colour for each heater state, as static maps from the
// state's name (svelte-5.md). The domain decides which state a reading is in;
// this file is the only place that decides how it reads on screen.
import type { HeaterState } from '@prusa/connect-ui/domain';
import type { BadgeVariant } from '@prusa/connect-ui/primitives';

/**
 * The catalogue key the badge reads. This module ships `locales/<lang>.json`
 * beside its entry and the host merges it under this module's namespace, so
 * the word itself is read when the badge draws (frontend-i18n.md rule 7).
 */
export const HEATER_WORD_KEY: Record<HeaterState, string> = {
  heating: 'nozzle.state.heating',
  'at-temperature': 'nozzle.state.atTemperature',
  cooling: 'nozzle.state.cooling',
  off: 'nozzle.state.off',
  'not-reported': 'nozzle.state.notReported',
};

/**
 * What colour the badge is. `cooling` is a warning and not a quiet grey: a
 * nozzle above the cooling floor is still hot enough to burn somebody reaching
 * into the machine, which is what the domain's floor is there for.
 *
 * `secondary` is the library's quiet fill, which the two states nobody has to
 * act on take.
 */
export const HEATER_VARIANT: Record<HeaterState, BadgeVariant> = {
  heating: 'info',
  'at-temperature': 'success',
  cooling: 'warning',
  off: 'secondary',
  'not-reported': 'secondary',
};
