export * from './data';
export * from './domain';
export * from './i18n';
export * from './primitives';
export * from './slots';
// Both entries name a CardProps. The root keeps the Card primitive's, which
// it exported first, and names a dashboard card's props after the dashboard;
// a module reads the card contract from the slots entry under its own name.
export type { CardProps } from './primitives';
export type { CardProps as DashboardCardProps } from './slots';
export { applySkin, DEFAULT_SKIN, type Skin, SKIN_NAMES } from './styles/skin';
export {
  skin,
  SKIN_STORAGE_KEY,
  type SkinStorage,
  SkinStore,
  type SkinStoreOptions,
} from './styles/skin-store.svelte';
export {
  applyTheme,
  THEME_PREFERENCES,
  THEME_STORAGE_KEY,
  theme,
  type ThemeMedia,
  type ThemePreference,
  type ThemeStorage,
  ThemeStore,
  type ThemeStoreOptions,
} from './styles/theme.svelte';
