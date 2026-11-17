// The glyph of each card in the gallery. A module's card names one of the
// icons the library publishes (ADR-20260911-4p6 rule 15). The core's cards are not
// held to that list and name none in declarations.ts, so this file is where
// each one gets a glyph that shows what it is.
import type { LucideIcon } from '@lucide/svelte';
import ActivityIcon from '@lucide/svelte/icons/activity';
import CameraIcon from '@lucide/svelte/icons/camera';
import ClockIcon from '@lucide/svelte/icons/clock';
import FlagIcon from '@lucide/svelte/icons/flag';
import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
import { PrinterGlyphIcon } from '../../../primitives';
import StickyNoteIcon from '@lucide/svelte/icons/sticky-note';
import ThermometerIcon from '@lucide/svelte/icons/thermometer';
import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

import { moduleIcon } from '../../../slots';

const CORE_ICONS: Readonly<Record<string, LucideIcon>> = {
  'core/printer': PrinterGlyphIcon,
  'core/printers': LayoutGridIcon,
  'core/summary': ActivityIcon,
  'core/finishing': FlagIcon,
  'core/temperature': ThermometerIcon,
  'core/camera': CameraIcon,
  'core/note': StickyNoteIcon,
  'core/clock': ClockIcon,
  'core/issues': TriangleAlertIcon,
};

/** The glyph a card type is shown with. */
export const cardIcon = (type: string, icon: string | undefined): LucideIcon =>
  (Object.hasOwn(CORE_ICONS, type) ? CORE_ICONS[type] : undefined) ??
  moduleIcon(icon);
