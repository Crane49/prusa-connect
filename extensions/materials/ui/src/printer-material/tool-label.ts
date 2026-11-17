// The name a person reads for a printer tool.
import type { Translate } from '@prusa/connect-ui/i18n';

/** Tool 1 for tool 0, and Slot 4 for MMU slot 0.3: Core counts from 0. */
export const toolLabel = (t: Translate, id: string): string => {
  const [head = '0', slot] = id.split('.');
  return slot === undefined
    ? t('materials.printer.tool', { n: Number(head) + 1 })
    : t('materials.printer.slot', { n: Number(slot) + 1 });
};
