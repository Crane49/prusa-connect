import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export interface ValuePreset {
  /** The value this preset commits, `0` for an `Off` preset. */
  value: number;
  /** The word on the button: a material name, `Off`, `100%`. */
  label: string;
}

/** One of several things the editor can set, offered before its controls. */
export interface ValueChoice {
  /** Unique among the choices. */
  key: string;
  /** Which one it is, `Tool 1`. */
  subject: string;
  /** Its live reading, already formatted by the caller. */
  currentLabel: string;
  /** The value the controls start from once it is picked. */
  target: number;
  /** Names the target field, `Target for T1 (°C)`. */
  targetLabel?: string | undefined;
}

export interface ValueEditorProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /** What is being set: `Nozzle`, `Bed`, `Speed`. Sentence case. */
  label: string;
  /** Which one, when a printer has more than one, `Tool 1`. */
  subject?: string | undefined;
  /** The live reading, already formatted by the caller, `234.6°C` or `42%`. */
  currentLabel: string;
  /** The live target this editor starts each session from. */
  target: number;
  /** Appended to the draft in the surface: `°C`, `%`. */
  unit: string;
  min: number;
  max: number;
  step?: number | undefined;
  presets?: ValuePreset[] | undefined;
  /** Whether this value can be set right now. */
  disabled?: boolean | undefined;
  /** Why the printer will not take this value at all. */
  reason?: string | undefined;
  trigger?: Snippet<[{ props: Record<string, unknown> }]> | undefined;
  /**
   * Given, the surface opens on these alone, and picking one opens the
   * controls for it with a way back.
   */
  choices?: ValueChoice[] | undefined;
  /** A choice's row; its `subject` alone when absent. */
  choice?: Snippet<[ValueChoice]> | undefined;
  /** Appends the live target to the trigger's accessible name and title. */
  announceTarget?: boolean | undefined;
  /** `choice` is the one picked, when the editor offers choices. */
  onCommit: (next: number, choice?: ValueChoice) => void;
  class?: ClassValue | undefined;
}
