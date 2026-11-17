import type { Snippet } from 'svelte';

import type { NavGroup } from '../../slots';
import type { ConnectionState } from '../connection-status';
import type { RailNavItem } from '../rail';

export interface PhoneBarProps {
  productName: string;
  items: readonly RailNavItem[];
  groups?: readonly NavGroup[] | undefined;
  settings?: RailNavItem | undefined;
  currentPath: string;
  connection: ConnectionState;
  principal?: string | undefined;
  /** A second line under the name in the account menu. */
  principalDetail?: string | undefined;
  onSignOut?: (() => void) | undefined;
  bell?: Snippet | undefined;
}
