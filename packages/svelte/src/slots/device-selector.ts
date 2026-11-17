// The devices selector of a slot handed a device (ADR-20261110-5np). An entry
// that carries one is shown only for a device whose record meets every
// condition it names. Today the one condition is resident: true, a printer the
// server runs on. With no record of the device yet, no such entry is shown, so
// it never appears on the wrong printer.

/** The conditions an entry's `devices` block names. Absent is every device. */
export interface DeviceSelector {
  /** True: only a device whose record has `resident: true`. */
  readonly resident?: boolean | undefined;
}

/** What the filter reads of a device record. The API's `Device` type fits it. */
export interface SlotDevice {
  readonly resident?: boolean | undefined;
}

/**
 * A slot target with the `device` prop added to each member that names a
 * device. A slot handed a file or nothing refuses the prop.
 */
export type WithSlotDevice<Target> = Target extends { deviceId: string }
  ? Target & {
      /**
       * The device's record. The outlet or the menu reads it only to decide
       * whether an entry with a devices selector is shown (ADR-20261110-5np).
       * The component still gets only the id. With no record, no such entry
       * is shown.
       */
      device?: SlotDevice | undefined;
    }
  : Target;

/** The conditions this version reads, each tested against a record. */
const conditions: Readonly<
  Record<string, (value: unknown, device: SlotDevice) => boolean>
> = {
  resident: (value, device) => value === true && device.resident === true,
};

/**
 * Whether an entry with this selector is shown for this device. Each
 * condition the selector names has to hold. A condition this version cannot
 * read, an unknown key or a value it does not take, counts as not met, so a
 * newer server's selector never shows an entry where it should not be. A
 * selector that names no condition is not met either. The server never sends
 * one, so it can only be a condition lost on the way.
 */
export const matchesDevice = (
  selector: DeviceSelector | undefined,
  device: SlotDevice | undefined,
): boolean => {
  if (selector === undefined) return true;
  if (device === undefined) return false;

  // The listing is read as JSON, and nothing checks the block's shape.
  const block: unknown = selector;
  if (typeof block !== 'object' || block === null) return false;

  const named = Object.entries(block).filter(
    ([, value]) => value !== undefined,
  );

  return (
    named.length > 0 &&
    named.every(([key, value]) => {
      const holds = Object.hasOwn(conditions, key)
        ? conditions[key]
        : undefined;

      return holds?.(value, device) === true;
    })
  );
};
