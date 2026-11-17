import type { PortalPairing } from '../../data';

/** What the wizard reads from and asks of the pairing store. */
export type PortalPairingSource = Pick<
  PortalPairing,
  | 'busy'
  | 'disconnect'
  | 'error'
  | 'markOpened'
  | 'opened'
  | 'phase'
  | 'retry'
  | 'start'
  | 'status'
  | 'watch'
>;

export interface PortalPairingWizardProps {
  pairing: PortalPairingSource;
}
