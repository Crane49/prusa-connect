import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

export interface DetailHeaderProps {
  /**
   * The subject's name, drawn as the page's `h1`. One face, clipped in the
   * middle: a slicer's file name is one unbroken word a hundred characters
   * long, and a name that wraps pushed everything beside it off its own line.
   * The end — the version suffix and the extension — is what a person reads
   * to tell two of them apart, so the end is what survives.
   */
  title: string;
  /** The subject's picture, at whatever size the caller's artwork needs. */
  media?: Snippet | undefined;
  /**
   * The one fact that belongs on the title's own line: a state badge, a
   * count. The title clips rather than wraps, so this keeps its place beside
   * the name however long the name is.
   */
  titleAside?: Snippet | undefined;
  /** The lines under the title: a state, a model, a legend. */
  meta?: Snippet | undefined;
  /** The acts offered on the subject. */
  actions?: Snippet | undefined;
  loading?: boolean | undefined;
  class?: ClassValue | undefined;
}
