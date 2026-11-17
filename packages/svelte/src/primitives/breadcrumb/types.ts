import type { Snippet } from 'svelte';
import type {
  HTMLAnchorAttributes,
  HTMLAttributes,
  HTMLLiAttributes,
  HTMLOlAttributes,
} from 'svelte/elements';

export interface BreadcrumbProps extends HTMLAttributes<HTMLElement> {
  /** What a screen reader calls this trail. The catalogue's word by default. */
  label?: string;
}

export type BreadcrumbListProps = HTMLOlAttributes;

export type BreadcrumbItemProps = HTMLLiAttributes;

export interface BreadcrumbLinkProps extends HTMLAnchorAttributes {
  /**
   * Renders the caller's own element instead of the `<a>`, for a router link
   * or a button. The snippet receives the attributes this component would
   * have set and must spread all of them. Nothing merges them for you: an
   * attribute the snippet leaves out is lost, and no error says so.
   */
  child?: Snippet<[{ props: Record<string, unknown> }]>;
}

export type BreadcrumbPageProps = HTMLAttributes<HTMLSpanElement>;

export type BreadcrumbSeparatorProps = HTMLLiAttributes;

export interface BreadcrumbEllipsisProps extends HTMLAttributes<HTMLSpanElement> {
  /**
   * The word a screen reader reads for the folded crumbs. The catalogue's word
   * by default.
   */
  moreText?: string;
}
