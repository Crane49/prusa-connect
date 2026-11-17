import type { ClassValue, HTMLImgAttributes } from 'svelte/elements';

/**
 * Every machine this product draws.
 *
 * `unknown` is a real member of the union rather than a fallback bolted on to
 * the end: a printer this instance cannot identify is an expected case, not a
 * failure, and it has artwork of its own.
 */
export const PRINTER_MODELS = [
  'core_one',
  'core_one_mmu3',
  'core_one_oak',
  'core_one_indx',
  'core_one_l',
  'core_one_l_mmu3',
  'core_one_l_indx',
  'mini',
  'mk25',
  'mk25s',
  'mk3',
  'mk3mmu2',
  'mk3s',
  'mk3smmu2s',
  'mk3smmu3',
  'mk35',
  'mk35mmu3',
  'mk35s',
  'mk35smmu3',
  'mk39',
  'mk39mmu3',
  'mk39s',
  'mk39smmu3',
  'mk4',
  'mk4mmu2s',
  'mk4mmu3',
  'mk4s',
  'mk4smmu2s',
  'mk4smmu3',
  'xl',
  'xlmultitool2',
  'xlmultitool3',
  'xlmultitool4',
  'xlmultitool5',
  'xl_enclosure',
  'xl_multitool_2_enclosure',
  'xl_multitool_3_enclosure',
  'xl_multitool_4_enclosure',
  'xl_multitool_5_enclosure',
  'ht90',
  'trilab_deltiq_2',
  'trilab_deltiq_2_plus',
  'unknown',
] as const;

export type PrinterModel = (typeof PRINTER_MODELS)[number];

// Prusa Connect draws these as PNG renders and has no vector source for them.
const RASTER_MODELS: ReadonlySet<PrinterModel> = new Set([
  'core_one_mmu3',
  'core_one_oak',
  'core_one_indx',
  'core_one_l_mmu3',
  'core_one_l_indx',
]);

/** The file a model is drawn from, which is named after the model. */
export const printerArtworkFile = (model: PrinterModel): string =>
  `${model}.${RASTER_MODELS.has(model) ? 'png' : 'svg'}`;

export interface PrinterIconProps extends Omit<
  HTMLImgAttributes,
  'alt' | 'class' | 'src'
> {
  model: PrinterModel;
  /**
   * The model as the device reports it. For a model drawn as `unknown`, the
   * icon looks it up among the models a driver declared and shows its name.
   */
  reported?: string | undefined;
  /**
   * Where the artwork is served from. An application serves the folder at this
   * path; a page under a different prefix passes its own. This is what `art`
   * does with its own drawings.
   */
  base?: string;
  class?: ClassValue | undefined;
}
