// What says a file reached the printer.

import { type PrintEnd, TRANSFER_RUNNING } from '../../data';
import { describeFault, t } from '../../i18n';
import { toast } from '../../primitives';
import type { PrinterTransferRow } from './status';

/**
 * Raises one toast per transfer that ends while the page is open.
 *
 * A landing happens away from the eye. By then the transfer's own row is gone
 * and the folder listing looks no different, so without this nothing on the
 * screen says the file arrived.
 *
 * The store is shared between pages, so its rows may come from an earlier
 * visit. Nothing is recorded until the page's own read of the listing has
 * answered, which the caller says with `read`. The rows of that read are
 * recorded without a toast. After it, a row is announced when it was seen
 * running and then ended, or when it first appears already ended. Switching
 * printers starts a fresh record.
 */
export class TransferAnnouncer {
  #printer = '';
  #seen = new Map<string, string>();
  #listed = false;

  observe(
    printer: string,
    rows: readonly PrinterTransferRow[],
    read: boolean,
  ): void {
    if (this.#printer !== printer) {
      this.#printer = printer;
      this.#seen = new Map();
      this.#listed = false;
    }
    if (!read) return;

    for (const row of rows) {
      const before = this.#seen.get(row.transferId);
      this.#seen.set(row.transferId, row.state);
      if (!this.#listed || TRANSFER_RUNNING.includes(row.state)) continue;

      const wasRunning =
        before !== undefined && TRANSFER_RUNNING.includes(before);
      if (before !== undefined && !wasRunning) continue;

      const name = row.path.split('/').pop() ?? row.path;
      if (row.state === 'done') {
        toast.success(t('printerDetailFiles.transfers.landed', { name }));
      } else if (row.state === 'failed') {
        // A transfer a person stopped, and one the machine cut off, both
        // already say so in the row they keep.
        // The row carries no fault code, so a refusal and a transfer that
        // broke on the way share one sentence with the server's reason.
        const reason = row.reason ?? '';
        toast.error(
          reason === ''
            ? t('printerDetailFiles.transfers.lost', { name })
            : t('printerDetailFiles.transfers.lostBecause', { name, reason }),
        );
      }
    }

    this.#listed = true;
  }

  /**
   * Says a file was not printed, with the reason the fault reads as. A file
   * the printer took and would not start is said to be on the printer, after
   * its landing; one whose request failed before the printer answered is only
   * said not to have printed.
   */
  notPrinted(name: string, ended: PrintEnd): void {
    if (ended.fault === undefined) return;

    const text = describeFault(ended.fault);
    const reason = [text.title, text.detail].filter(Boolean).join(': ');
    toast.error(
      ended.answer === undefined
        ? t('printerDetailFiles.transfers.notPrinted', { name, reason })
        : t('printerDetailFiles.transfers.notStartedBecause', { name, reason }),
    );
  }
}
