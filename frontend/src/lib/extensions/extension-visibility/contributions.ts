import type { ResourceInstance } from '@prusa/connect-ui/data';

// Every contribution the manifest declares, keyed as the server's
// hidden_contributions entries are: slot key, then the path of a page or the
// id of anything else.
const SLOT_KINDS = [
  ['pages', 'page'],
  ['device_card', 'deviceCard'],
  ['device_section', 'deviceSection'],
  ['device_tab', 'deviceTab'],
  ['device_action', 'deviceAction'],
  ['file_action', 'fileAction'],
  ['print_section', 'printSection'],
  ['settings', 'settings'],
] as const;

export interface Contribution {
  readonly key: string;
  readonly title: string;
  readonly kind: (typeof SLOT_KINDS)[number][1];
}

export const contributionsOf = (
  instance: ResourceInstance | undefined,
): Contribution[] =>
  SLOT_KINDS.flatMap(([slot, kind]) =>
    (instance?.ui?.contributes[slot] ?? []).map((one) => ({
      key: `${slot}/${'path' in one ? one.path : one.id}`,
      title: one.title,
      kind,
    })),
  );
