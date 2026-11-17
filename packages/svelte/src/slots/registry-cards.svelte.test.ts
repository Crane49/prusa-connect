import { describe, expect, it } from 'vitest';

import PlainCard from '../test/plain-card.svelte';
import type { CardDeclaration } from './card-types';
import { SlotRegistry, UndeclaredRegistration } from './registry.svelte';
import type { ModuleContributes } from './types';

const spoolCard = {
  id: 'spool',
  title: 'Spool',
  description: 'The spool on one printer.',
  size: { w: 4, h: 3 },
  least: { w: 2, h: 2 },
  settings: [
    { key: 'device', kind: 'printer' as const, label: 'Printer', default: '' },
  ],
};

const spools: ModuleContributes = { cards: [spoolCard] };

const clock: CardDeclaration = {
  id: 'clock',
  title: 'Clock',
  description: 'The time where the printers are.',
  size: { w: 2, h: 2 },
  least: { w: 2, h: 1 },
  settings: [],
};

const registryFor = () => {
  const registry = new SlotRegistry();
  registry.declare('spools', spools);

  return registry;
};

describe('SlotRegistry and dashboard cards', () => {
  it('lists a declared card before any bundle registers, and resolves none yet', () => {
    const registry = registryFor();

    expect(registry.declaredCards()).toEqual([
      { type: 'spools/spool', namespace: 'spools', declaration: spoolCard },
    ]);
    expect(registry.card('spools/spool')).toBeUndefined();
  });

  it('resolves a registered card by its type, with the manifest declaration', () => {
    const registry = registryFor();
    registry.registerCard('spools', 'spool', PlainCard);

    const card = registry.card('spools/spool');

    expect(card?.namespace).toBe('spools');
    expect(card?.declaration).toEqual(spoolCard);
    expect(card?.component).toBe(PlainCard);
    expect(card?.editor).toBeUndefined();
  });

  // ADR-20261008-sww rule 17: the manifest is the ceiling for cards too.
  it('refuses a card the manifest does not declare, and records it', () => {
    const registry = registryFor();

    expect(() => {
      registry.registerCard('spools', 'secret', PlainCard);
    }).toThrow(UndeclaredRegistration);
    expect(registry.card('spools/secret')).toBeUndefined();
    expect(registry.refusals()).toEqual([
      {
        namespace: 'spools',
        reason: 'undeclared',
        slot: 'dashboard-card',
        name: 'secret',
      },
    ]);
  });

  it('leaves a refused module out of the declared cards', () => {
    const registry = registryFor();
    registry.refuse({ namespace: 'spools', reason: 'threw', detail: 'boom' });

    expect(registry.declaredCards()).toEqual([]);
  });

  it('puts the core cards first, under core, and keeps them through forget and clear', () => {
    const registry = registryFor();
    registry.registerCoreCard(clock, PlainCard);
    registry.registerCard('spools', 'spool', PlainCard);

    expect(registry.declaredCards().map((card) => card.type)).toEqual([
      'core/clock',
      'spools/spool',
    ]);
    expect(registry.card('core/clock')?.namespace).toBe('core');
    expect(registry.card('spools/spool')?.namespace).toBe('spools');

    registry.forget('spools');
    expect(registry.card('spools/spool')).toBeUndefined();

    registry.clear();
    expect(registry.card('core/clock')?.namespace).toBe('core');
  });

  it('throws on a core card that breaks a rule, or that is registered twice', () => {
    const registry = new SlotRegistry();

    expect(() => {
      registry.registerCoreCard({ ...clock, least: { w: 3, h: 1 } }, PlainCard);
    }).toThrow(/least is larger than size/);

    registry.registerCoreCard(clock, PlainCard);
    expect(() => {
      registry.registerCoreCard(clock, PlainCard);
    }).toThrow(/twice/);
  });
});

describe('a module card registered twice', () => {
  it('throws on the second registration and keeps the first', () => {
    const registry = registryFor();
    registry.registerCard('spools', 'spool', PlainCard);
    const first = registry.card('spools/spool');

    expect(() => {
      registry.registerCard('spools', 'spool', PlainCard);
    }).toThrow(/spools\/spool is registered twice/);
    expect(registry.card('spools/spool')).toBe(first);
    expect(registry.refusals()).toEqual([]);
  });
});

describe('the name of the module a card comes from', () => {
  it('is the title of its first page, and nothing for a module with none', () => {
    const registry = new SlotRegistry();
    registry.declare('com.example.parts', {
      pages: [{ path: 'parts', title: 'Parts', hidden: false }],
    });
    registry.declare('com.example.quiet', {});

    expect(registry.moduleTitle('com.example.parts')).toBe('Parts');
    expect(registry.moduleTitle('com.example.quiet')).toBeUndefined();
  });
});
