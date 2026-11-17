import { t } from '../../i18n';
import type { PrinterModel } from './types';

/** A model that names a real product, which is every model but `unknown`. */
type KnownModel = Exclude<PrinterModel, 'unknown'>;

// A product name reads the same in every language and is not a text
// (frontend-i18n.md rule 10), so the real machines are named here. An
// unrecognised printer names no product, and its two names are catalogue keys.
const PRODUCT_NAME: Record<KnownModel, string> = {
  core_one: 'Prusa CORE One',
  core_one_mmu3: 'Prusa CORE One with MMU3',
  core_one_oak: 'Prusa CORE One Signature Oak',
  core_one_indx: 'Prusa CORE One INDX',
  core_one_l: 'Prusa CORE One L',
  core_one_l_mmu3: 'Prusa CORE One L with MMU3',
  core_one_l_indx: 'Prusa CORE One L INDX',
  mini: 'Original Prusa MINI+',
  mk25: 'Original Prusa MK2.5',
  mk25s: 'Original Prusa MK2.5S',
  mk3: 'Original Prusa MK3',
  mk3mmu2: 'Original Prusa MK3 with MMU2',
  mk3s: 'Original Prusa MK3S+',
  mk3smmu2s: 'Original Prusa MK3S+ with MMU2S',
  mk3smmu3: 'Original Prusa MK3S+ with MMU3',
  mk35: 'Original Prusa MK3.5',
  mk35mmu3: 'Original Prusa MK3.5 with MMU3',
  mk35s: 'Original Prusa MK3.5S',
  mk35smmu3: 'Original Prusa MK3.5S with MMU3',
  mk39: 'Original Prusa MK3.9',
  mk39mmu3: 'Original Prusa MK3.9 with MMU3',
  mk39s: 'Original Prusa MK3.9S',
  mk39smmu3: 'Original Prusa MK3.9S with MMU3',
  mk4: 'Original Prusa MK4',
  mk4mmu2s: 'Original Prusa MK4 with MMU2S',
  mk4mmu3: 'Original Prusa MK4 with MMU3',
  mk4s: 'Original Prusa MK4S',
  mk4smmu2s: 'Original Prusa MK4S with MMU2S',
  mk4smmu3: 'Original Prusa MK4S with MMU3',
  xl: 'Original Prusa XL',
  xlmultitool2: 'Original Prusa XL, 2 tool heads',
  xlmultitool3: 'Original Prusa XL, 3 tool heads',
  xlmultitool4: 'Original Prusa XL, 4 tool heads',
  xlmultitool5: 'Original Prusa XL, 5 tool heads',
  xl_enclosure: 'Original Prusa XL with enclosure',
  xl_multitool_2_enclosure: 'Original Prusa XL, 2 tool heads, with enclosure',
  xl_multitool_3_enclosure: 'Original Prusa XL, 3 tool heads, with enclosure',
  xl_multitool_4_enclosure: 'Original Prusa XL, 4 tool heads, with enclosure',
  xl_multitool_5_enclosure: 'Original Prusa XL, 5 tool heads, with enclosure',
  ht90: 'Prusa Pro HT90',
  trilab_deltiq_2: 'Trilab DeltiQ 2',
  trilab_deltiq_2_plus: 'Trilab DeltiQ 2 Plus',
};

// `xlmultitool*` keeps its tool count: a real, visible difference.
const SHORT_NAME: Record<KnownModel, string> = {
  core_one: 'CORE One',
  core_one_mmu3: 'CORE One MMU3',
  core_one_oak: 'CORE One Oak',
  core_one_indx: 'CORE One INDX',
  core_one_l: 'CORE One L',
  core_one_l_mmu3: 'CORE One L MMU3',
  core_one_l_indx: 'CORE One L INDX',
  mini: 'MINI+',
  mk25: 'MK2.5',
  mk25s: 'MK2.5S',
  mk3: 'MK3',
  mk3mmu2: 'MK3 MMU2',
  mk3s: 'MK3S+',
  mk3smmu2s: 'MK3S+ MMU2S',
  mk3smmu3: 'MK3S+ MMU3',
  mk35: 'MK3.5',
  mk35mmu3: 'MK3.5 MMU3',
  mk35s: 'MK3.5S',
  mk35smmu3: 'MK3.5S MMU3',
  mk39: 'MK3.9',
  mk39mmu3: 'MK3.9 MMU3',
  mk39s: 'MK3.9S',
  mk39smmu3: 'MK3.9S MMU3',
  mk4: 'MK4',
  mk4mmu2s: 'MK4 MMU2S',
  mk4mmu3: 'MK4 MMU3',
  mk4s: 'MK4S',
  mk4smmu2s: 'MK4S MMU2S',
  mk4smmu3: 'MK4S MMU3',
  xl: 'XL',
  xlmultitool2: 'XL 2-Tool',
  xlmultitool3: 'XL 3-Tool',
  xlmultitool4: 'XL 4-Tool',
  xlmultitool5: 'XL 5-Tool',
  xl_enclosure: 'XL Enclosure',
  xl_multitool_2_enclosure: 'XL 2-Tool Enclosure',
  xl_multitool_3_enclosure: 'XL 3-Tool Enclosure',
  xl_multitool_4_enclosure: 'XL 4-Tool Enclosure',
  xl_multitool_5_enclosure: 'XL 5-Tool Enclosure',
  ht90: 'HT90',
  trilab_deltiq_2: 'DeltiQ 2',
  trilab_deltiq_2_plus: 'DeltiQ 2 Plus',
};

/**
 * What the icon says to a screen reader: the machine's full product name. The
 * icon is how a card identifies its machine, so the name is the whole of it.
 */
export const printerModelName = (model: PrinterModel): string =>
  model === 'unknown'
    ? t('primitives.printerIcon.unknownName')
    : PRODUCT_NAME[model];

/**
 * The shorter name for text beside the icon. At `size-14` the artwork tells
 * one family from another but not one model within a family, so a card that
 * has to say MK4S rather than MK4 says it in words.
 */
export const printerModelLabel = (model: PrinterModel): string =>
  model === 'unknown'
    ? t('primitives.printerIcon.unknownLabel')
    : SHORT_NAME[model];
