// Moved from pull request 185, packages/screens/src/slots.ts, 2026-09-11
import type { components } from '@prusa/connect';

/**
 * What a module may register, as `GET /api/v1/resources` carries it beside
 * `openapi_url` (ADR-20260911-4p6 rule 32). It is the manifest's `contributes` block,
 * validated by the core at install, so the frontend reads it and does not
 * parse a manifest of its own.
 */
export type ModuleContributes = components['schemas']['ResourceContributes'];

/**
 * What the frontend needs to load one module: where its entry module is
 * served, the library range it compiled against, and what it may register.
 */
export type ModuleUI = components['schemas']['ResourceUI'];

export {
  type ActionOutletTarget,
  type ActionRuns,
  type ActionSlot,
  buildRegister,
  type Icon,
  MODULE_ICONS,
  type ModuleRegister,
  type RegisterHandlers,
  type Screen,
  type SectionOutletTarget,
  type SectionProps,
  type SectionSlot,
  SLOT_FORMS,
  SLOT_KEYS,
  SLOT_NAMES,
  SLOT_SCREENS,
  type SlotName,
  type SlotsOf,
} from './generated';
