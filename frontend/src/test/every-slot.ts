// A fake module that fills every slot one screen carries, so a test can draw
// that screen and look for each slot's outlet without hand-writing a
// registration per slot.
import type { Component } from 'svelte';

import {
  SLOT_FORMS,
  SLOT_KEYS,
  SLOT_NAMES,
  SLOT_SCREENS,
  type ActionSlot,
  type ModuleContributes,
  type Screen,
  type SectionProps,
  type SectionSlot,
  type SlotRegistry,
} from '@prusa/connect-ui/slots';

import PlainPage from './plain-page.svelte';
import PlainPane from './plain-pane.svelte';
// Not exported from the package: the outlet tests reach for the same fixture
// the registry's own tests use, rather than keep a second copy in sync.
import PlainCard from '../../../packages/svelte/src/test/plain-card.svelte';
import PlainSection from '../../../packages/svelte/src/test/plain-section.svelte';

export const EVERY_SLOT_NAMESPACE = 'test.every';
export const EVERY_SLOT_ENTRY = 'every';

/**
 * Declares and registers one entry of every slot `screen` carries, driven by
 * `SLOT_FORMS` and `SLOT_KEYS` rather than a branch per slot name: a new
 * device-subject section or action slot on an existing screen needs no edit
 * here. `sidebar` is derived from a page registration and has no key or
 * registration of its own, so it is skipped.
 */
export const declareEverySlot = (
  registry: SlotRegistry,
  screen: Screen,
): void => {
  const slots = SLOT_NAMES.filter(
    (name) => SLOT_SCREENS[name] === screen && SLOT_FORMS[name] !== 'derived',
  );
  const title = 'Every slot';

  const contributes = Object.fromEntries(
    slots.map((name) => {
      switch (SLOT_FORMS[name]) {
        case 'page':
          return ['pages', [{ path: EVERY_SLOT_ENTRY, title, hidden: false }]];
        case 'card':
          return [
            'cards',
            [
              {
                id: EVERY_SLOT_ENTRY,
                title,
                description: 'A card for the outlet test.',
                size: { w: 2, h: 2 },
                least: { w: 1, h: 1 },
                settings: [],
              },
            ],
          ];
        case 'action':
          return [
            SLOT_KEYS[name as ActionSlot],
            [{ id: EVERY_SLOT_ENTRY, title, destructive: false }],
          ];
        default:
          return [
            SLOT_KEYS[name as SectionSlot],
            [{ id: EVERY_SLOT_ENTRY, title }],
          ];
      }
    }),
  ) as ModuleContributes;

  registry.declare(EVERY_SLOT_NAMESPACE, contributes);

  for (const name of slots) {
    switch (SLOT_FORMS[name]) {
      case 'page':
        registry.registerPage(
          EVERY_SLOT_NAMESPACE,
          EVERY_SLOT_ENTRY,
          PlainPage,
        );
        break;
      case 'card':
        registry.registerCard(
          EVERY_SLOT_NAMESPACE,
          EVERY_SLOT_ENTRY,
          PlainCard,
        );
        break;
      case 'action':
        registry.registerAction(
          EVERY_SLOT_NAMESPACE,
          name as ActionSlot,
          EVERY_SLOT_ENTRY,
          () => Promise.resolve(),
        );
        break;
      default:
        // Every section-form slot that carries the device subject needs a
        // component that reads `deviceId`; the others, `settings` and
        // `print-section`, draw one that reads nothing.
        registry.registerSection(
          EVERY_SLOT_NAMESPACE,
          name as SectionSlot,
          EVERY_SLOT_ENTRY,
          // Widened the same way the registry's own registerSection widens a
          // caller's component: the branch below is chosen at runtime, so
          // its static type cannot narrow with `name`.
          (name.startsWith('device') ? PlainSection : PlainPane) as Component<
            SectionProps[SectionSlot]
          >,
        );
    }
  }
};
