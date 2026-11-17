import type { ValuePreset } from '../primitives/value-editor';

/**
 * The nozzle presets a value editor is shown with. PLA and PVB hold the same
 * figure, which is what proves a highlight keyed on the label rather than on
 * the value.
 */
export const NOZZLE_PRESETS: ValuePreset[] = [
  { value: 0, label: 'Off' },
  { value: 215, label: 'PLA' },
  { value: 230, label: 'PETG' },
  { value: 260, label: 'ASA' },
  { value: 275, label: 'PC' },
  { value: 215, label: 'PVB' },
  { value: 255, label: 'ABS' },
  { value: 220, label: 'HIPS' },
  { value: 240, label: 'PP' },
  { value: 240, label: 'FLEX' },
  { value: 285, label: 'PA' },
];
