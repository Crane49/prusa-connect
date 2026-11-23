import type { FaultText } from '@prusa/connect-ui/i18n';
import type { ModuleHost } from '@prusa/connect-ui/slots';

/** Translates one of this module's texts. */
export type Translate = ModuleHost['t'];

/** Where the API key of this section is. */
export type KeyState =
  | { kind: 'none' }
  | { kind: 'creating' }
  | { kind: 'created'; token: string }
  | { kind: 'failed'; fault: FaultText };
