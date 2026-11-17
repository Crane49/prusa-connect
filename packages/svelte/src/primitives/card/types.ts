import type { HTMLAttributes } from 'svelte/elements';

// Every part is a div. A card carries no role of its own: a caller that wants
// one names it, as the landmark story does with `role="region"`.

/** `critical` and `attention` tint the border and fill with that severity. */
export type CardTone = 'default' | 'critical' | 'attention';

/**
 * The padding and gap between parts. `default` steps up with the card's own
 * width, `compact` and `tight` stay fixed, and `flush` has none for a card
 * whose content draws to the edge.
 */
export type CardDensity = 'default' | 'compact' | 'tight' | 'flush';

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  tone?: CardTone;
  density?: CardDensity;
}

export type CardHeaderProps = HTMLAttributes<HTMLDivElement>;

export type CardTitleProps = HTMLAttributes<HTMLDivElement>;

export type CardDescriptionProps = HTMLAttributes<HTMLDivElement>;

export type CardActionProps = HTMLAttributes<HTMLDivElement>;

export type CardContentProps = HTMLAttributes<HTMLDivElement>;

export type CardFooterProps = HTMLAttributes<HTMLDivElement>;
