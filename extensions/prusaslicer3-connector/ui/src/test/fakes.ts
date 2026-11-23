// What the stories and the tests draw instead of the server and the slicer:
// the module's English texts, printers in the shape the route gives, and a
// window that plays PrusaSlicer.
import {
  type FaultText,
  locale,
  registerCatalogue,
  translateFor,
} from '@prusa/connect-ui/i18n';
import {
  createSlicer,
  type Slicer,
  type SlicerMessage,
  type SlicerWindow,
} from '@prusa/connect-ui/slicer';

import type { SlicerPrinter, SlicerTool, Translate } from '../api';
import {
  candidatesFor,
  preselected,
  type PrintRequest,
} from '../compatibility';
import english from '../locales/en.json';
import type { Load, PickerView } from '../select-printer.svelte';

const NAMESPACE = 'com.github.aux.prusa3d.prusaslicer3-connector';
registerCatalogue(NAMESPACE, 'en', english);
locale.set('en');

/** The module's English texts, with the params filled in. */
export const t: Translate = translateFor(NAMESPACE);

/** A filament printer with one tool per entry. */
export const printer = (
  uuid: string,
  name: string,
  model: string,
  state: string,
  tools: { nozzle?: number; material?: string; hardened?: boolean }[] = [
    { nozzle: 0.4, material: 'PLA' },
  ],
): SlicerPrinter => ({
  uuid,
  name,
  connect_state: state,
  legacy_printer_model: model,
  tool_count: tools.length,
  tools: Object.fromEntries(
    tools.map((tool, index): [string, SlicerTool] => [
      String(index),
      {
        class: 'FFF',
        type: 'PRINT_HEAD',
        features: {
          ...(tool.nozzle === undefined
            ? {}
            : { nozzle_diameter: tool.nozzle }),
          nozzle_hardened: tool.hardened ?? false,
          nozzle_high_flow: false,
        },
        ...(tool.material === undefined
          ? {}
          : {
              material_package_instance: {
                package: { material: { type: tool.material } },
              },
            }),
      },
    ]),
  ),
});

export const farm: SlicerPrinter[] = [
  printer('mk4s-1', 'Workshop MK4S', 'MK4S', 'READY'),
  printer('mk4s-2', 'Office MK4S', 'MK4S', 'PRINTING'),
  printer('mk4s-3', 'Lab MK4S', 'MK4S', 'IDLE', [
    { nozzle: 0.6, material: 'PETG' },
  ]),
  printer('mk4s-4', 'Garage MK4S', 'MK4S', 'OFFLINE'),
  printer('xl-1', 'Big XL', 'XL', 'READY', [
    { nozzle: 0.4, material: 'PLA' },
    { nozzle: 0.4, material: 'PETG' },
  ]),
];

/** What PrusaSlicer 3 sends for a one-bed MK4S project in PLA. */
export const mk4sRequest: PrintRequest = {
  printerUuid: 'mk4s-1',
  filename: 'bracket_0.4n_0.2mm_PLA_MK4S_1h.bgcode',
  printerModel: 'MK4S',
  materials: ['PLA'],
  nozzleDiameters: [0.4],
  hardened: [false],
  highFlow: [false],
};

export const failed: FaultText = {
  code: 'core.http/INTERNAL',
  title: 'The server failed',
  detail: 'read the printers: database is locked',
  action: undefined,
  traceId: undefined,
};

/** A print dialog in one state, for a story or a page test. */
export const pickerOf = (
  options: {
    load?: Load;
    inSlicer?: boolean;
    request?: PrintRequest | undefined;
    printers?: SlicerPrinter[];
    filename?: string;
    sent?: boolean;
  } = {},
): PickerView & { posted: string[] } => {
  const request = 'request' in options ? options.request : mk4sRequest;
  const printers = options.printers ?? farm;
  const candidates =
    request === undefined ? [] : candidatesFor(request, printers);
  const load = options.load ?? 'ready';
  const posted: string[] = [];
  let filename = options.filename ?? request?.filename ?? '';
  let chosen: string | undefined;

  return {
    posted,
    inSlicer: options.inSlicer ?? true,
    load,
    fault: load === 'failed' || load === 'refused' ? failed : undefined,
    request,
    candidates,
    noPrinters: printers.length === 0,
    get selected() {
      return (
        chosen ??
        (request === undefined ? undefined : preselected(request, candidates))
      );
    },
    get filename() {
      return filename;
    },
    set filename(next: string) {
      filename = next;
    },
    sent: options.sent ?? false,
    get ready() {
      return (
        this.selected !== undefined &&
        (request?.multipleBeds === true || filename.trim() !== '')
      );
    },
    refresh: () => {
      posted.push('refresh');

      return Promise.resolve();
    },
    select: (uuid: string) => {
      chosen = uuid;
    },
    print: () => {
      posted.push('PRINT');
    },
    cancel: () => {
      posted.push('CLOSE_DIALOG');
    },
  };
};

/** A window with PrusaSlicer in it: what the page posts, and a way to call it. */
export interface FakeSlicer {
  slicer: Slicer;
  sent: SlicerMessage[];
}

export const fakeSlicer = (inSlicer = true): FakeSlicer => {
  const target = new EventTarget();
  const sent: SlicerMessage[] = [];
  const page: SlicerWindow = {
    _prusaSlicer: inSlicer
      ? {
          postMessage: (message) => {
            sent.push(message);
          },
        }
      : undefined,
    addEventListener: (type, listener) => {
      target.addEventListener(type, listener);
    },
    removeEventListener: (type, listener) => {
      target.removeEventListener(type, listener);
    },
    dispatchEvent: (event) => target.dispatchEvent(event),
  };

  return { slicer: createSlicer(() => page), sent };
};
