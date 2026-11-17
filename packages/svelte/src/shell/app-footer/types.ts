export interface AppFooterProps {
  host?: string | undefined;
  appVersion?: string | undefined;
  /** Where this server serves its API reference. Undefined on a build that leaves the page out. */
  apiReference?: string | undefined;
  /** Where this build's documentation is read. Undefined on a build that names none. */
  documentation?: string | undefined;
  /** Where the Support link points. Undefined keeps the project's issue tracker. */
  supportUrl?: string | undefined;
}
