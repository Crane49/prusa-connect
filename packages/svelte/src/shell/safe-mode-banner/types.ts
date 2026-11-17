import type { ServerRestartRefusal } from '../../data';

export interface SafeModeBannerProps {
  /** When safe mode began, as an ISO timestamp. */
  since: string;
  /** The restart was asked for and the server has not answered yet. */
  restarting: boolean;
  /** The server did not come back within the wait. */
  timedOut: boolean;
  /** Whether the person may restart the server; only true shows the button. */
  allowed?: boolean | undefined;
  /** Why the server refused the restart, when it did. */
  refusal?: ServerRestartRefusal | undefined;
  /** What a failed restart request threw. */
  error?: unknown;
  onRestartNormally: () => void;
}
