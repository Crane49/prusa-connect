// What says how a command a person sent ended.

import type { CommandEntry } from '../../data';
import { describeFault, t } from '../../i18n';
import { toast } from '../../primitives';
import { verbText } from './commands/vocabulary';
import {
  elapsedText,
  RESULT_KEY,
  resultOf,
  resultText,
} from './history/command-status';

/**
 * The sentence for an argument a driver may drop, in words a person knows.
 * An argument missing here is named as the printer's wire spells it.
 */
const IGNORED: Readonly<Record<string, () => string>> = {
  filament: () => t('printerDetailCommands.ignoredFilament'),
};

const ignoredText = (wire: string): string =>
  IGNORED[wire]?.() ?? t('printerDetailCommands.ignored', { name: wire });

/** The sentences under the title, or no description when there are none. */
const described = (
  lines: readonly (string | undefined)[],
): { description?: string } => {
  const kept = lines.filter((line): line is string => line !== undefined);

  return kept.length === 0 ? {} : { description: kept.join(' · ') };
};

/**
 * Raises one toast for each command a person sent: done, refused with the
 * reason, or ended in a way nobody saw. A G-code line is announced only when
 * the printer refused it, because the console shows its answer under the
 * field.
 *
 * A person pressing a control on the Now or Controls tab is not looking at the
 * command log on the Advanced tab, so the toast is the only place the outcome
 * reaches them.
 */
export class CommandAnnouncer {
  #printer = '';
  #told = -1;

  observe(printer: string, last: CommandEntry | undefined): void {
    if (this.#printer !== printer) {
      this.#printer = printer;
      this.#told = -1;
    }

    if (last === undefined || last.id === this.#told) return;
    this.#told = last.id;
    if (last.quiet === true) return;

    const name = last.kind === 'command' ? verbText(last.sent) : last.sent;
    const took =
      last.elapsedMs === undefined
        ? undefined
        : t('printerDetailCommands.took', {
            duration: elapsedText(Math.round(last.elapsedMs)),
          });

    if (last.unknown === true) {
      toast.warning(
        t('printerDetailCommands.ended', {
          verb: name,
          result: t(RESULT_KEY.unknown),
        }),
        described([t('printerDetailCommands.unknown'), took]),
      );

      return;
    }

    if (last.ok) {
      if (last.kind !== 'command') return;

      const changed = [
        ...last.ignored.map(ignoredText),
        ...Object.entries(last.substituted ?? {}).map(([wire, value]) =>
          t('printerDetailCommands.substituted', { name: wire, value }),
        ),
      ];
      const title = t('printerDetailCommands.ended', {
        verb: name,
        result: t(RESULT_KEY.succeeded),
      });
      const options = described([...changed, took]);
      if (changed.length === 0) toast.success(title, options);
      else toast.warning(title, options);

      return;
    }

    // A command record may end in a word other than a refusal, such as timed
    // out, and that word is the title.
    if (
      last.status !== undefined &&
      resultOf({ status: last.status }) !== 'refused'
    ) {
      toast.error(
        t('printerDetailCommands.ended', {
          verb: name,
          result: resultText({ status: last.status }),
        }),
        described([last.reason, last.code, took]),
      );

      return;
    }

    if (last.code === undefined) {
      toast.error(
        t('printerDetailCommands.refusedPlain'),
        described([last.reason, took]),
      );

      return;
    }

    // The title is the catalogue's line for the code, and the printer's own
    // words stay under it in the language the printer used.
    const fault = describeFault({ code: last.code, message: last.reason });
    toast.error(
      fault.title,
      described([
        fault.detail === undefined
          ? last.code
          : t('printerDetailCommands.refusedDetail', {
              detail: fault.detail,
              code: last.code,
            }),
        took,
      ]),
    );
  }
}
