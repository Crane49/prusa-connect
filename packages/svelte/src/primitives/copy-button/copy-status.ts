import { t } from '../../i18n';
import type { CopyStatus } from './types';

const MESSAGE_KEY = {
  pending: 'primitives.copying',
  copied: 'primitives.copied',
  failed: 'primitives.copyFailed',
} as const;

/**
 * What the copy did, in words.
 *
 * `idle` says nothing. A button that announces "not copied yet" reads as a
 * failure to a screen reader arriving cold.
 *
 * The catalogue is read on every call, so a caller wraps this in a `$derived`
 * and a change of language rewrites the sentence (frontend-i18n.md rule 1).
 */
export const copyStatusMessage = (status: CopyStatus): string =>
  status === 'idle' ? '' : t(MESSAGE_KEY[status]);
