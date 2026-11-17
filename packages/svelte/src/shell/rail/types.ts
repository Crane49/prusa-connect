import type { Snippet } from 'svelte';

import type { NavGroup, NavItem } from '../../slots';

export interface RailNavItem extends NavItem {
  children?: readonly NavItem[] | undefined;
}

export interface RailProps {
  items: readonly RailNavItem[];
  groups?: readonly NavGroup[] | undefined;
  currentPath: string;
  collapsed?: boolean | undefined;
  onToggle?: (() => void) | undefined;
  settings?: RailNavItem | undefined;
  footer?: Snippet | undefined;
}

/** A path below href also counts; the root only matches exactly. */
export const isCurrent = (href: string, path: string): boolean => {
  if (href === path) return true;
  if (href === '/') return false;
  return path.startsWith(`${href}/`);
};

/** False if one of the item's own children also matches the path. */
export const isCurrentRow = (item: RailNavItem, path: string): boolean =>
  isCurrent(item.href, path) &&
  !(item.children ?? []).some((child) => isCurrent(child.href, path));

/** The item itself unless one of its children shares its href, then each child. */
export const sectionEntries = (item: RailNavItem): readonly RailNavItem[] => {
  const children = item.children ?? [];
  return children.some((child) => child.href === item.href)
    ? children
    : [item, ...children];
};
