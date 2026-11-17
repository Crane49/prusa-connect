import type { BadgeSize, BadgeVariant } from './types';

// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

// The focus ring is Button's recipe. A bare span never takes focus, and the
// ring is here because the `child` snippet can draw an anchor or a button.
export const base =
  'inline-flex w-fit shrink-0 items-center justify-center gap-2 overflow-hidden rounded-sm border px-2 text-xs font-medium whitespace-nowrap transition-colors outline-none [button&]:cursor-pointer focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background [&>svg]:pointer-events-none [&>svg]:size-3';

// `[a&]` narrows a rule to the badge drawn as an anchor. A badge that is only
// a label has nothing to hover, so the hover fills apply to the link form
// alone.
//
// `success`, `warning` and `info` are filled with their own token under a
// black foreground: success 6.27:1, warning 15.59:1 light and 13.30:1 dark,
// info 5.36:1. White fails all three. `destructive` takes
// `text-destructive-foreground` for the same reason, at 5.11:1 light and
// 4.83:1 dark.
//
// The six `state*` entries come from the --state-* tint pairs in tokens.css: a
// step-3 fill under its own hue's step-11 text, which is why they carry no
// hover and no -foreground token. Each paints a 4px left border in its
// family's -edge token instead of matching its fill all round, so its box is
// 3px wider than a success or warning one. They only ever render beside each
// other.
export const variants: Record<BadgeVariant, string> = {
  default:
    'border-primary bg-primary text-primary-foreground [a&]:hover:bg-primary-hovered',
  secondary:
    'border-secondary bg-secondary text-secondary-foreground [a&]:hover:bg-secondary-hovered',
  outline:
    'border-border bg-transparent text-foreground [a&]:hover:bg-accent [a&]:hover:text-foreground [button&]:hover:bg-accent [button&]:hover:text-foreground',
  success: 'border-success bg-success text-success-foreground',
  warning: 'border-warning bg-warning text-warning-foreground',
  info: 'border-info bg-info text-info-foreground',
  destructive:
    'border-destructive bg-destructive text-destructive-foreground [a&]:hover:bg-destructive-hovered',
  stateActive:
    'border-state-active border-l-4 border-l-state-active-edge bg-state-active text-state-active-text',
  stateDone:
    'border-state-done border-l-4 border-l-state-done-edge bg-state-done text-state-done-text',
  stateIdle:
    'border-state-idle border-l-4 border-l-state-idle-edge bg-state-idle text-state-idle-text',
  stateAttention:
    'border-state-attention border-l-4 border-l-state-attention-edge bg-state-attention text-state-attention-text',
  stateFault:
    'border-state-fault border-l-4 border-l-state-fault-edge bg-state-fault text-state-fault-text',
  stateQuiet:
    'border-state-quiet border-l-4 border-l-state-quiet-edge bg-state-quiet text-state-quiet-text',
};

export const sizes: Record<BadgeSize, string> = {
  sm: 'py-0.5',
  md: 'py-1',
  // `text-sm!`: `base` carries `text-xs`, and the later of two font sizes wins
  // by stylesheet order, not by where the class sits.
  lg: 'gap-3 py-1.5 ps-1.5 pe-4 text-sm!',
};

export const mono = 'font-mono leading-none tracking-wide uppercase';

export const numeric = 'font-mono tabular-nums';

export const isState = (variant: BadgeVariant): boolean =>
  variant.startsWith('state');
