import type { ToggleSize, ToggleVariant } from './types';

// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

// What both variants share, including the pressed look.
//
// Three selectors are compound on purpose. A plain `disabled:bg-disabled` ties
// with `data-[state=on]:bg-selected` on specificity and loses, so a disabled
// pressed toggle would keep the brand fill and read as usable. A plain
// `active:text-secondary-foreground` ties with `data-[state=on]:text-selected-foreground`
// the same way, and what a tie loses to there is the selected colour on itself.
//
// `cursor-pointer` closes the Tailwind v4 Preflight gap that `button.svelte`
// closes too, with the same `disabled:cursor-default` override.
export const base =
  "inline-flex cursor-pointer items-center justify-center gap-2 text-sm font-medium whitespace-nowrap transition-colors outline-none disabled:pointer-events-none disabled:cursor-default disabled:text-disabled-foreground disabled:data-[state=on]:border-transparent disabled:data-[state=on]:bg-disabled disabled:data-[state=on]:text-disabled-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background data-[state=on]:border-selected data-[state=on]:bg-selected data-[state=on]:text-selected-foreground data-[state=on]:hover:border-selected-hovered data-[state=on]:hover:bg-selected-hovered data-[state=on]:active:bg-selected-hovered data-[state=on]:active:text-selected-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";

// The corner, apart from the rest. Nothing here merges conflicting utilities,
// so a component that takes this recipe and squares its corners, as
// `toggle-group-item` does, has to be able to leave this one out: two
// border-radius utilities on one element are settled by the order of the
// stylesheet and `rounded-sm` is the later of the two.
export const radius = 'rounded-sm';

// The resting pair is Button's `ghost`: `bg-transparent` on its own left the
// text unreadable in the dark theme. The `active:` fill is for the off state,
// and it is what says a tap landed on a tablet, where `hover:` never fires.
// secondary-foreground on secondary measures 9.98:1 light and 11.98:1 dark.
export const variants: Record<ToggleVariant, string> = {
  default:
    'border border-transparent bg-transparent text-muted-foreground hover:bg-accent hover:text-foreground active:bg-secondary active:text-secondary-foreground',
  outline:
    'border border-border bg-transparent text-muted-foreground hover:bg-accent hover:text-foreground active:bg-secondary active:text-secondary-foreground',
};

// The heights are Button's. `min-w-*` holds a one-character label to the same
// minimum touch target as a wide one.
export const sizes: Record<ToggleSize, string> = {
  sm: 'h-7 min-w-7 px-2 text-xs',
  md: 'h-9 min-w-9 px-2.5 text-sm',
  lg: 'h-11 min-w-11 px-3 text-base',
};
