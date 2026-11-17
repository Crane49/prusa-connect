import type {
  DataFault,
  PortalPairingPhase,
  PortalPairingStatus,
} from '../../data';
import type { PortalPairingSource } from './types';

/** A pairing store with fixed answers, for stories and component tests. */
export const pairingSource = (
  over: Partial<PortalPairingSource> = {},
): PortalPairingSource => ({
  status: { state: 'unpaired' },
  error: undefined,
  phase: 'intro',
  busy: undefined,
  opened: false,
  start: () => Promise.resolve(),
  retry: () => Promise.resolve(),
  disconnect: () => Promise.resolve(),
  markOpened: () => undefined,
  watch: () => () => undefined,
  ...over,
});

const inFifteenMinutes = (): string =>
  new Date(Date.now() + 14 * 60_000 + 30_000).toISOString();

/** A code just handed out, with the address that carries it. */
export const pendingStatus = (): PortalPairingStatus => ({
  state: 'pending',
  step: 'awaiting_confirmation',
  user_code: 'WDJB-MJHT',
  verification_uri: 'https://beta.prusa3d.com/device',
  verification_uri_complete:
    'https://beta.prusa3d.com/device?user_code=WDJB-MJHT',
  expires_at: inFifteenMinutes(),
  interval_ms: 5000,
});

export const portalFault = (code: string, message: string): DataFault => ({
  code,
  status: 502,
  params: {},
  message,
  traceId: undefined,
});

/** One fixed source per phase, so every phase has a story. */
export const phaseSource = (phase: PortalPairingPhase): PortalPairingSource => {
  switch (phase) {
    case 'loading':
      return pairingSource({ status: undefined, phase });
    case 'code':
      return pairingSource({ status: pendingStatus(), phase });
    case 'confirm':
      return pairingSource({ status: pendingStatus(), phase, opened: true });
    case 'approval':
      return pairingSource({
        status: { ...pendingStatus(), step: 'awaiting_approval' },
        phase,
        opened: true,
      });
    case 'paired':
      return pairingSource({
        status: {
          state: 'paired',
          identity: { subject: 'tester-1', name: 'Ada Lovelace' },
          product: { slug: 'prusa-connect', name: 'Prusa Connect' },
          device: { identifier: 'instance-1' },
        },
        phase,
      });
    case 'expired':
    case 'denied':
      return pairingSource({ status: { state: phase }, phase });
    case 'revoked':
      return pairingSource({
        status: { state: 'revoked', identity: { name: 'Ada Lovelace' } },
        phase,
      });
    case 'clock':
      return pairingSource({
        phase,
        error: portalFault(
          'core.resource/BETA_PORTAL_CLOCK_SKEW',
          "this server's clock differs from the Beta Portal's by 2h0m0s",
        ),
      });
    case 'unreachable':
      return pairingSource({
        phase,
        error: portalFault(
          'core.resource/BETA_PORTAL_UNAVAILABLE',
          'the Beta Portal is unavailable',
        ),
      });
    case 'intro':
      return pairingSource();
  }
};
