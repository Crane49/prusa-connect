export interface FirstRunScreenProps {
  /** The server being set up, drawn under the title. */
  host?: string | undefined;
  /** True while the decision is being recorded. */
  busy?: boolean | undefined;
  /** The last refusal, as it was thrown. Rendered through `describeFault`. */
  error?: unknown;
  /**
   * Records the decision: the first administrator's name and password, and
   * the one-time setup code the server printed in its log.
   */
  onCreate: (username: string, password: string, code: string) => void;
}
