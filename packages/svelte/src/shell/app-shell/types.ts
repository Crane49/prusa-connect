import type { Snippet } from 'svelte';

import type { Notices } from '../../data';
import type { NavGroup, NavItem } from '../../slots';
import type { ConnectionState } from '../connection-status';
import type { RailNavItem } from '../rail';
import type { IssueSummary, NoticeHref } from './notice-item';
import type { SendsSource } from '../sends-indicator';

/** What the bell reads of the notices store. */
export type NoticeSource = Pick<
  Notices,
  'items' | 'toggleRead' | 'markAllRead'
>;

export interface AppShellProps {
  productName: string;
  navItems: readonly NavItem[];
  navGroups?: readonly NavGroup[] | undefined;
  settings?: RailNavItem | undefined;
  currentPath: string;
  /** Where the brand mark leads. */
  homeHref?: string | undefined;
  connection: ConnectionState;
  principal?: string | undefined;
  /** A second line under the name in the account menu, such as an address. */
  principalDetail?: string | undefined;
  onSignOut?: (() => void) | undefined;
  host?: string | undefined;
  appVersion?: string | undefined;
  apiReference?: string | undefined;
  documentation?: string | undefined;
  supportUrl?: string | undefined;
  /** The bell's list. Left out, the bell draws an empty one. */
  notifications?: NoticeSource | undefined;
  /**
   * Where a notice leads, such as the page that approves a waiting printer.
   * A notice it answers nothing for offers no action.
   */
  noticeHref?: NoticeHref | undefined;
  /** What a printer's issue reason means, for the notice that announces it. */
  issueSummary?: IssueSummary | undefined;
  /** The files this tab is putting on printers. Left out, nothing is drawn. */
  sends?: SendsSource | undefined;
  /** A strip across the top of the page column, above the page. */
  banner?: Snippet | undefined;
  children: Snippet;
}
