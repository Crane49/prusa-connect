// What the Tools tab shows of one tool, worked out once from the tool
// the module answers and the spool on it, so each layout only draws it.
import type { Translate } from '@prusa/connect-ui/i18n';

import type { Assignment, Spool, Tool } from '../api';

export interface ToolView {
  tool: Tool;
  /** Extruder on a one-tool printer, Tool 1 on another, S1 for an MMU slot. */
  label: string;
  /** The type in the circle: what the printer reports, else what is assigned. */
  circle: string;
  colorRgba: string | undefined;
  /** The material's name, undefined when no material is assigned. */
  name: string | undefined;
  brand: string;
  /** The spool's grams left and when full, when Materials knows both. */
  leftG: number | undefined;
  fullG: number | undefined;
  fromMaterials: boolean;
  ring: 'none' | 'still' | 'turning';
}

/** The first text that is not empty, and '' when none is. */
export const firstText = (...texts: (string | undefined)[]): string =>
  texts.find((text) => text !== undefined && text !== '') ?? '';

/**
 * The active tool's ring: it turns while printing. Out of a print, an active
 * slot keeps a still ring and a head shows none.
 */
const ringOf = (tool: Tool, printing: boolean): ToolView['ring'] => {
  if (!tool.active || (tool.kind !== 'slot' && !printing)) return 'none';
  return printing ? 'turning' : 'still';
};

/** A printer's assignment without a spool may have no name. */
export const nameOf = (a: Assignment): string =>
  a.material_name !== ''
    ? a.material_name
    : [...new Set([a.abbreviation, a.type])]
        .filter((s) => s !== '')
        .join(' · ');

/**
 * The label on a tool's tile: "S n" for an MMU slot, "Extruder" for the only
 * tool of a printer, and "Tool n" for one of several.
 */
export const tileLabel = (
  t: Translate,
  tool: Tool,
  single: boolean,
): string => {
  const [head = '0', slot] = tool.id.split('.');
  if (slot !== undefined)
    return t('materials.tools.slot', { n: Number(slot) + 1 });
  return single
    ? t('materials.tools.extruder')
    : t('materials.printer.tool', { n: Number(head) + 1 });
};

export const toolView = (
  t: Translate,
  tool: Tool,
  spool: Spool | undefined,
  single: boolean,
  printing: boolean,
): ToolView => {
  const a = tool.assignment;
  // A printer's report alone names no material.
  const named = a !== null && a.material_uuid !== '';
  const colour = firstText(a?.color_rgba, tool.reported?.color_rgba);
  const full =
    spool?.full_weight_g === undefined
      ? undefined
      : spool.full_weight_g - (spool.empty_weight_g ?? 0);

  return {
    tool,
    label: tileLabel(t, tool, single),
    circle: firstText(tool.reported?.abbreviation, a?.abbreviation, a?.type),
    colorRgba: colour === '' ? undefined : colour,
    name: named ? nameOf(a) : undefined,
    brand: named ? a.brand_name : '',
    leftG: spool?.remaining_weight_g,
    fullG: full !== undefined && full > 0 ? full : undefined,
    fromMaterials: a?.spool_id !== undefined,
    ring: ringOf(tool, printing),
  };
};

/** The share of a spool left, 0 to 1, when both weights are known. */
export const shareLeft = (view: ToolView): number | undefined =>
  view.leftG === undefined || view.fullG === undefined
    ? undefined
    : Math.min(1, Math.max(0, view.leftG / view.fullG));
