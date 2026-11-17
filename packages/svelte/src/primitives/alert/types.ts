import type { HTMLAttributes } from 'svelte/elements';

/**
 * The looks an alert takes. `default` is a plain notice; the other four carry
 * the status colour of what they report.
 */
export type AlertVariant =
  'default' | 'destructive' | 'success' | 'warning' | 'info';

export interface AlertProps extends HTMLAttributes<HTMLDivElement> {
  variant?: AlertVariant;
}

export type AlertTitleProps = HTMLAttributes<HTMLDivElement>;

export type AlertDescriptionProps = HTMLAttributes<HTMLDivElement>;
