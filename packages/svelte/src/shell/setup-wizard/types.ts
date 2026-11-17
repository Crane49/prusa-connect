import type { SetupFlow } from '../../data';

export interface SetupWizardProps {
  /** The conversation with the server: the configuration, the confirm, the wait. */
  flow: SetupFlow;
  /** The server being set up, drawn beside the steps. */
  host?: string | undefined;
  /** The build the server runs. */
  version?: string | undefined;
  /** Where the documentation is. It opens in a new tab. */
  docsHref?: string | undefined;
  /** Switches the page's language and keeps the choice. */
  onlanguage: (tag: string) => void;
  /** The start that failed after the wizard was last confirmed, when it did. */
  lastFailure?: string | undefined;
  /**
   * Leaves the wizard once the server has recorded the decision. The
   * administrator's name and password are handed over so the page signs in
   * without asking again; they are `undefined` when nobody signs in.
   */
  onfinish: (
    credentials: { username: string; password: string } | undefined,
  ) => Promise<void>;
}
