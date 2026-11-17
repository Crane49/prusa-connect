import type { LucideIcon } from '@lucide/svelte';
import PuzzleIcon from '@lucide/svelte/icons/puzzle';

import { type Icon, MODULE_ICONS } from './generated';

/**
 * One destination in the primary navigation. The host builds the href: a
 * module names a path, and the namespace it lands under is derived from
 * where the module was installed (ADR-20260911-4p6 rule 15).
 */
export interface NavItem {
  id: string;
  label: string;
  href: string;
  /**
   * Which group the sidebar draws it under, by group id. An item with no
   * group is drawn above the first group, with no heading over it.
   */
  group?: string | undefined;
  /** The glyph beside the label. One family at one stroke. */
  icon?: LucideIcon | undefined;
  /** One step in, for a page that belongs to the item above it: a module's page under Extensions. */
  nested?: boolean | undefined;
}

/**
 * A heading over a run of navigation items.
 *
 * `NavItem.group` names one by id. The label comes from the application,
 * because each product spells its own catalogue keys (ADR-20260915-4an rule 2).
 */
export interface NavGroup {
  id: string;
  label: string;
}

/**
 * The application's own destinations, then a module's. Always that order: a
 * module able to sort itself above the fleet would be changing a page it does
 * not own, which is the one thing the slot rules exist to prevent (ADR-20260911-4p6
 * rule 9).
 */
export const withModuleNav = (
  own: readonly NavItem[],
  modules: readonly NavItem[],
): NavItem[] => [...own, ...modules];

/** The icon a page gets when its manifest names none, or a name not listed. */
export const DEFAULT_MODULE_ICON: LucideIcon = PuzzleIcon;

/**
 * The icon for one name a manifest wrote. It reads own keys only, so a name
 * such as `constructor` gets the default and not a property of every object.
 * An installed manifest from before the icon check may still name any
 * string, so a name the generated set does not carry falls to the default
 * rather than failing to render.
 */
export const moduleIcon = (name: string | undefined): LucideIcon =>
  name !== undefined && Object.hasOwn(MODULE_ICONS, name)
    ? // Object.hasOwn narrows nothing for TypeScript; the check above is what
      // makes this cast true (typescript.md).
      MODULE_ICONS[name as Icon]
    : DEFAULT_MODULE_ICON;
