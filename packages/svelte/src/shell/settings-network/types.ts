import type { DataFault, Visibility } from '../../data';

/** The time server switch, as the network room reads and changes it. */
export interface TimeServerSource {
  /** True when the switch is on. */
  readonly enabled: boolean;
  /** What the time server does: serving, off or failed. */
  readonly state: string | undefined;
  /** Why it failed, as the operating system said it. */
  readonly reason: string | undefined;
  /** What the failure was, when known: permission or in_use. */
  readonly cause: string | undefined;
  /** The UDP port a device asks on. */
  readonly port: number | undefined;
  /** True once the server has answered. */
  readonly loaded: boolean;
  /** True while a change is on its way. It holds the switch. */
  readonly saving: boolean;
  /** What the last read or change was refused with. */
  readonly error: DataFault | undefined;
  /** Starts or stops the time server. */
  setEnabled(enabled: boolean): Promise<void>;
}

/** The hidden setting, as the network room reads and changes it. */
export interface VisibilitySource {
  /** True when this server announces nothing. */
  readonly hidden: boolean;
  /** What the announcer does: announcing, hidden, off or failed. */
  readonly announcement: string | undefined;
  /** What the server announces, once it has answered. */
  readonly described: Visibility | undefined;
  /** True once the server has answered. */
  readonly loaded: boolean;
  /** True while a change is on its way. It holds the switch. */
  readonly saving: boolean;
  /** What the last read or change was refused with. */
  readonly error: DataFault | undefined;
  /** Hides the server, or shows it again. */
  setHidden(hidden: boolean): Promise<void>;
}

export interface SettingsNetworkScreenProps {
  source: VisibilitySource;
  /** The time server switch. Absent, the room draws no time server section. */
  timeServer?: TimeServerSource;
  /** The documentation root, which a failure links into. */
  docsHref?: string | undefined;
}
