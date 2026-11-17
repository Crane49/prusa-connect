/** The filament materials this product knows. */

export interface PreheatPreset {
  /** The firmware's own name for the material, shown as-is. */
  material: string;
  nozzleCelsius: number;
  bedCelsius: number;
}

/** In the order the printer's own preheat menu lists them. */
export const PREHEAT_PRESETS: readonly PreheatPreset[] = [
  { material: 'PLA', nozzleCelsius: 215, bedCelsius: 60 },
  { material: 'PETG', nozzleCelsius: 230, bedCelsius: 85 },
  { material: 'ASA', nozzleCelsius: 260, bedCelsius: 100 },
  { material: 'ABS', nozzleCelsius: 255, bedCelsius: 100 },
  { material: 'PC', nozzleCelsius: 275, bedCelsius: 100 },
  { material: 'FLEX', nozzleCelsius: 240, bedCelsius: 50 },
  { material: 'HIPS', nozzleCelsius: 220, bedCelsius: 100 },
  { material: 'PP', nozzleCelsius: 240, bedCelsius: 100 },
  { material: 'PVB', nozzleCelsius: 215, bedCelsius: 75 },
  // The firmware asks 280 of a MINI and 285 of every other board.
  { material: 'PA', nozzleCelsius: 285, bedCelsius: 100 },
];
