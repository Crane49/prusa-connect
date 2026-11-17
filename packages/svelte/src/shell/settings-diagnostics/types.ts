import type { DiagnosticsChoice } from '../../data/services/diagnostics';

export type { DiagnosticsChoice };

/** What the person says the sent file is for. The portal opens on it. */
export type SendIntent = 'new' | 'existing';

export interface SettingsDiagnosticsScreenProps {
  /** Fetches and saves the bundle. A throw is shown as a fault. */
  onDownload: (choice: DiagnosticsChoice) => Promise<void>;
  /**
   * The title of each installed extension that adds files to the bundle.
   * Empty or absent draws no choice for them.
   */
  extensions?: readonly string[] | undefined;
  /** The browser's own details, as text for a ticket. */
  onCopy: () => string;
  /**
   * Builds the bundle and sends it to the support portal. Absent when this
   * server sends nowhere, and then the screen offers the download alone. A
   * throw is shown as a fault.
   */
  onSend?:
    | ((choice: DiagnosticsChoice, intent: SendIntent) => Promise<void>)
    | undefined;
}
