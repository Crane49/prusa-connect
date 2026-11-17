import type { PrusaConnect, EventType, StreamEvent } from '@prusa/connect';
import { SvelteMap } from 'svelte/reactivity';

import {
  type DeviceStorages,
  writableStorageOf,
} from '../domain/printer-storage';
import { deviceIdOf, type Fleet, withdrawnOriginOf } from './fleet.svelte';
import { readDeviceStorages } from './services/print';

/**
 * The type the server publishes when a printer's storages change, which is a
 * USB drive put in or pulled out. A host adds it to the one stream it opens.
 */
export const DEVICE_STORAGE_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.storages.changed',
];

/**
 * What a storages event says of one printer, or undefined for any other
 * event. The payload crosses into this code as `unknown`, so it is narrowed to
 * the two fields read here (typescript.md).
 */
export const storagesOfEvent = (
  event: StreamEvent,
): DeviceStorages | undefined => {
  if (!DEVICE_STORAGE_EVENTS.includes(event.type)) return undefined;

  const payload = event.payload as
    { items?: unknown; reported?: unknown } | null | undefined;
  if (typeof payload !== 'object' || payload === null) return undefined;
  if (!Array.isArray(payload.items)) return undefined;

  return {
    items: payload.items as DeviceStorages['items'],
    reported: payload.reported === true,
  };
};

// The printer answered on this connection and has a drive it can write to.
const takesFiles = (held: DeviceStorages | undefined): boolean =>
  held?.reported === true && writableStorageOf(held.items ?? []) !== undefined;

/** What the store reads of the SDK. */
export type PrinterStoragesClient = Pick<PrusaConnect, 'listDeviceStorages'>;

/**
 * What each printer said its storages are, for a screen that offers many
 * printers at once, such as the print picker.
 *
 * Nothing is read until a screen asks for a printer, and the server is then
 * asked to ask the printer again, because what it remembers predates any USB
 * drive put in since. The storages event keeps an answer current after that.
 */
export class PrinterStorages {
  readonly #client: PrinterStoragesClient;
  #held = new SvelteMap<string, DeviceStorages>();
  // The read in flight for each printer. Not state: nothing draws it.
  #flights: Record<string, Promise<void> | undefined> = {};

  constructor(client: PrinterStoragesClient) {
    this.#client = client;
  }

  /** What one printer said, or undefined before it was read. */
  of(deviceId: string): DeviceStorages | undefined {
    return this.#held.get(deviceId);
  }

  /**
   * True only once the printer answered and has no storage it can write to.
   * A printer not read yet, or one that has not answered, is not refused on a
   * guess.
   */
  noStorage(deviceId: string): boolean {
    const held = this.#held.get(deviceId);

    return held?.reported === true && !takesFiles(held);
  }

  /**
   * Asks the printers again. A printer already known to have a drive it can
   * write to is left alone, so opening the picker puts no frame on the wire
   * for a fleet that is ready. A failed read leaves the printer unknown: the
   * print itself asks again and says why it cannot start.
   */
  check(deviceIds: readonly string[]): Promise<void> {
    const asked = deviceIds
      .filter((id) => !takesFiles(this.#held.get(id)))
      .map((id) => this.#read(id));

    return Promise.all(asked).then(() => undefined);
  }

  /** Takes each storages event the fleet applies. The returned function stops it. */
  watch(fleet: Pick<Fleet, 'subscribe'>): () => void {
    return fleet.subscribe((event) => {
      const gone = withdrawnOriginOf(event);
      if (gone?.kind === 'device') this.#held.delete(gone.id);

      const storages = storagesOfEvent(event);
      const id = deviceIdOf(event);
      if (storages !== undefined && id !== undefined)
        this.#held.set(id, storages);
    });
  }

  reset(): void {
    this.#held.clear();
    this.#flights = {};
  }

  #read(deviceId: string): Promise<void> {
    const flying = this.#flights[deviceId];
    if (flying !== undefined) return flying;

    const flight = readDeviceStorages(this.#client, deviceId, { refresh: true })
      .then(
        (answer) => {
          if (this.#flights[deviceId] === flight) {
            this.#held.set(deviceId, answer);
          }
        },
        () => undefined,
      )
      .finally(() => {
        if (this.#flights[deviceId] === flight)
          this.#flights[deviceId] = undefined;
      });
    this.#flights[deviceId] = flight;

    return flight;
  }
}
