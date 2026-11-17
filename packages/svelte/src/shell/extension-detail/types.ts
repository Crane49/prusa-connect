import type { Snippet } from 'svelte';

import type { DetailTab } from '../../primitives/detail-tabs';
import type { ResourceOption } from './module-settings';

export type ExtensionObserved =
  | 'running'
  | 'degraded'
  | 'starting'
  | 'retrying'
  | 'stopped'
  | 'switched-off'
  | 'failed';

export interface ExtensionDetailScreenProps {
  title: string;
  version: string;
  /** Who the module is from, already worded: Local, Marketplace, Installed by hand. */
  source: string;
  /**
   * The module's path, such as github.com/acme/spools: the name every address
   * of the module is derived from. It names the module and is no link.
   */
  module?: string | undefined;
  observed: ExtensionObserved;
  /** Why it last stopped or failed, as the server words it. */
  result?: string | undefined;
  /** The on/off switch; the app draws it because it decides what switching does. */
  control?: Snippet | undefined;
  actions?: Snippet | undefined;
  tabs: readonly DetailTab[];
  tab: string;
  onTabChange: (tab: string) => void;
  extensionsHref: string;
  loading?: boolean | undefined;
}

export interface ModuleSettingsFormProps {
  options: readonly ResourceOption[];
  /** What the server holds; a stored secret arrives as the marker. */
  config: Readonly<Record<string, unknown>>;
  /** Why the settings cannot be changed, already worded. */
  locked?: string | undefined;
  saving: boolean;
  fault?: unknown;
  /** The module changed on the server since the form was read. */
  conflict?: boolean | undefined;
  /** Resolves true once the server applied the config. */
  onsave: (config: Record<string, unknown>) => Promise<boolean>;
  onreload: () => void;
  /** Tells whether the form holds edits the server has not applied. */
  ondirty?: ((dirty: boolean) => void) | undefined;
}
