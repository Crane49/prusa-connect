import type { ButtonIconSize, ButtonTextSize, ButtonVariant } from './types';

// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

// `cursor-pointer` closes a gap Tailwind v4's Preflight opened when it dropped
// the `button { cursor: pointer }` reset. `disabled:cursor-default` overrides
// it: `disabled:pointer-events-none` already stops the cursor from flipping on
// hover, but `getComputedStyle` reports the declared value whatever
// hit-testing does, so the override is what makes it measurable.
export const base =
  "inline-flex items-center justify-center gap-2 whitespace-nowrap shrink-0 cursor-pointer rounded-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:cursor-default [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";

// A link sits in running text, so it flows and wraps with it instead of being
// a flex box that keeps to one line. It has a base of its own rather than
// classes stacked over `base`: no tailwind-merge resolves conflicting
// utilities. The shared tail is repeated because the class-parity test reads
// only string literals.
export const linkBase =
  "inline whitespace-normal shrink-0 cursor-pointer rounded-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:cursor-default [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";

// Every boxed variant (all but `link`) paints an `active:` fill, and each one
// differs from that variant's `hover:` fill as well as from its resting one. The tablet this UI
// is opened on has no hover at all, so without a press fill Stop and Delete
// printer answer a tap with nothing, and a command that takes about a second
// gets tapped twice.
//
// `destructive` is an outline, and its resting text is `text-destructive-text`
// rather than `text-destructive`: --destructive measures 4.48:1 against --card
// and --popover, under WCAG AA's 4.5:1, while --destructive-text clears every
// surface. `ghost` rests on `text-muted-foreground`, because inheriting the
// body colour reads black in the dark theme.
export const variants: Record<ButtonVariant, string> = {
  default:
    'border border-primary bg-primary text-primary-foreground hover:border-primary-hovered hover:bg-primary-hovered active:border-transparent active:bg-primary-pressed disabled:border-transparent aria-disabled:border-transparent disabled:bg-disabled aria-disabled:bg-disabled disabled:text-disabled-foreground aria-disabled:text-disabled-foreground',
  secondary:
    'border border-secondary bg-secondary text-secondary-foreground hover:border-primary hover:bg-secondary-hovered active:border-primary active:bg-primary active:text-primary-foreground disabled:border-transparent aria-disabled:border-transparent disabled:bg-disabled aria-disabled:bg-disabled disabled:text-disabled-foreground aria-disabled:text-disabled-foreground',
  ghost:
    'border border-transparent bg-transparent text-muted-foreground hover:bg-accent hover:text-foreground active:bg-secondary active:text-secondary-foreground disabled:text-disabled-foreground aria-disabled:text-disabled-foreground',
  destructive:
    'border border-destructive bg-transparent text-destructive-text hover:bg-destructive hover:text-destructive-foreground active:bg-destructive-hovered active:text-destructive-foreground disabled:border-transparent aria-disabled:border-transparent disabled:bg-disabled aria-disabled:bg-disabled disabled:text-disabled-foreground aria-disabled:text-disabled-foreground',
  link: 'text-link underline-offset-4 hover:underline disabled:text-disabled-foreground aria-disabled:text-disabled-foreground',
};

// A square size carries no horizontal padding: the glyph is centred by the
// base utilities and the box is as tall as it is wide. `lg` and `icon-lg`
// measure 44px, the floor for a control operated with a gloved hand on a
// wall-mounted tablet.
export const sizes: Record<ButtonTextSize | ButtonIconSize, string> = {
  sm: 'h-7 px-2.5 text-xs',
  md: 'h-9 px-4 text-sm',
  lg: 'h-11 px-6 text-base',
  'icon-sm': 'size-7 text-sm',
  'icon-md': 'size-9 text-sm',
  'icon-lg': 'size-11 text-sm',
};

// A link has no box: it takes the surrounding text's size and line, so `size`
// does not apply to it. `base` carries no font size for the same reason.
export const linkSize = 'h-auto p-0';

// A link with an icon is a flex row, so the glyph and the words sit on one
// line, and the words wrap inside the row's width.
export const iconLinkBase =
  "inline-flex max-w-full items-center gap-1.5 whitespace-normal shrink-0 cursor-pointer rounded-sm text-start font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:cursor-default [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";
