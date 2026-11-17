import type { LucideIcon } from '@lucide/svelte';
import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
import Rows3Icon from '@lucide/svelte/icons/rows-3';
import Table2Icon from '@lucide/svelte/icons/table-2';
import VideoIcon from '@lucide/svelte/icons/video';

import { t } from '../../i18n';
import type { ViewModeName, ViewModeOption } from './types';

/** The one glyph and the one name each shape carries, wherever it appears. */
const CATALOGUE: Record<ViewModeName, { key: string; icon: LucideIcon }> = {
  table: { key: 'primitives.viewMode.table', icon: Table2Icon },
  rows: { key: 'primitives.viewMode.rows', icon: Rows3Icon },
  cards: { key: 'primitives.viewMode.cards', icon: LayoutGridIcon },
  frames: { key: 'primitives.viewMode.frames', icon: VideoIcon },
};

/** Dense first, roomy last, the cameras after all of them. */
const ORDER: ViewModeName[] = ['table', 'rows', 'cards', 'frames'];

/**
 * The options for a listing that offers these shapes, always in the canonical
 * order whatever order they are asked for.
 *
 * `unavailableReason` is the screen's own, since a table needs width this
 * listing may not have, so it is applied here rather than carried in the
 * catalogue.
 *
 * The names are read from the catalogue as this is called. A caller that
 * wants them to follow a change of language calls it inside a `$derived`.
 */
export const viewModes = (
  names: ViewModeName[],
  unavailableReason?: Partial<Record<ViewModeName, string | undefined>>,
): ViewModeOption[] =>
  ORDER.filter((name) => names.includes(name)).map((name) => ({
    value: name,
    label: t(CATALOGUE[name].key),
    icon: CATALOGUE[name].icon,
    unavailableReason: unavailableReason?.[name],
  }));
