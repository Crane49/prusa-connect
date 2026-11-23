// Which printers can take the file PrusaSlicer is about to send. The rules
// follow what Prusa Connect shows when PrusaSlicer opens it, so a person sees
// the same list here as in the cloud.
import type {
  CompatiblePrinterParams,
  SlicerMessage,
} from '@prusa/connect-ui/slicer';

import type { SlicerPrinter } from './api';

/** What the slicer asked for, in one shape whatever version of it sent it. */
export interface PrintRequest {
  printerUuid?: string;
  filename?: string;
  printerModel?: string;
  /** One material per tool. */
  materials?: string[];
  /** One nozzle diameter per tool. */
  nozzleDiameters?: number[];
  /** Per tool: the material is abrasive, so the nozzle must be hardened. */
  hardened?: boolean[];
  /** Per tool: the print needs a high-flow nozzle. */
  highFlow?: boolean[];
  /** The project has more than one bed. */
  multipleBeds?: boolean;
}

const strings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'string');
const numbers = (value: unknown): value is number[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'number');
const booleans = (value: unknown): value is boolean[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'boolean');

/**
 * The request in one shape. PrusaSlicer before 2.8.1 sends one material as a
 * string, sometimes joined with `;`, and one nozzle diameter as a number. A
 * field of the wrong type is left out, as the cloud leaves it out.
 */
export const normalizeRequest = (
  params: CompatiblePrinterParams,
): PrintRequest => {
  const request: PrintRequest = {};
  if (typeof params.printerUuid === 'string') {
    request.printerUuid = params.printerUuid;
  }
  if (typeof params.filename === 'string') request.filename = params.filename;
  if (typeof params.printerModel === 'string') {
    request.printerModel = params.printerModel;
  }
  const material: unknown = params.material;
  if (strings(material)) request.materials = material;
  else if (typeof material === 'string') {
    request.materials = material.split(';');
  }
  if (typeof params.nozzleDiameter === 'number') {
    request.nozzleDiameters = [params.nozzleDiameter];
  }
  if (numbers(params.nozzle_diameter)) {
    request.nozzleDiameters = params.nozzle_diameter;
  }
  if (booleans(params.filament_abrasive)) {
    request.hardened = params.filament_abrasive;
  }
  if (booleans(params.high_flow)) request.highFlow = params.high_flow;
  if (typeof params.multiple_beds === 'boolean') {
    request.multipleBeds = params.multiple_beds;
  }

  return request;
};

/** Why a printer may not suit the file. */
export type Incompatibility =
  | 'printerModel'
  | 'nozzleDiameter'
  | 'material'
  | 'filamentAbrasive'
  | 'nozzleHighFlow';

/** One tool, as the checks read it. */
export interface Tool {
  nozzleDiameter: number | undefined;
  hardened: boolean | undefined;
  highFlow: boolean | undefined;
  material: string | undefined;
}

/** The printer's tools, in the order of their index. */
export const toolsOf = (printer: SlicerPrinter): Tool[] =>
  Object.entries(printer.tools)
    .sort(([a], [b]) => Number(a) - Number(b))
    .map(([, tool]) => ({
      nozzleDiameter: tool.features?.nozzle_diameter,
      hardened: tool.features?.nozzle_hardened,
      highFlow: tool.features?.nozzle_high_flow,
      material: tool.material_package_instance?.package?.material?.type,
    }));

/** The model name the printer answers to, the one PrusaSlicer writes first. */
export const modelOf = (printer: SlicerPrinter): string | undefined =>
  printer.legacy_printer_model ?? printer.model;

const same = (a: string, b: string): boolean =>
  a.trim().toUpperCase() === b.trim().toUpperCase();

/**
 * Every way the printer does not suit the request. The model is compared with
 * the printer's `legacy_printer_model` and its `model`; `base_model` is not
 * read, because a file for one model does not print on its base model.
 */
export const incompatibilities = (
  request: PrintRequest,
  printer: SlicerPrinter,
): Incompatibility[] => {
  const found: Incompatibility[] = [];
  const wanted = request.printerModel;
  if (
    wanted !== undefined &&
    wanted !== '' &&
    ![printer.legacy_printer_model, printer.model].some(
      (model) => model !== undefined && same(model, wanted),
    )
  ) {
    found.push('printerModel');
  }

  const tools = toolsOf(printer);
  if (tools.length === 0) return found;

  const loaded = tools
    .map((tool) => tool.material)
    .filter((material) => material !== undefined && material !== '');
  if (
    request.materials !== undefined &&
    loaded.length > 0 &&
    request.materials.some((material) => !loaded.includes(material))
  ) {
    found.push('material');
  }
  if (
    request.nozzleDiameters?.some((diameter) =>
      tools.every(
        (tool) =>
          tool.nozzleDiameter !== undefined && tool.nozzleDiameter !== diameter,
      ),
    ) === true
  ) {
    found.push('nozzleDiameter');
  }
  if (
    request.hardened?.some(Boolean) === true &&
    tools.every((tool) => tool.hardened === false)
  ) {
    found.push('filamentAbrasive');
  }
  if (
    request.highFlow?.some(Boolean) === true &&
    tools.every((tool) => tool.highFlow === false)
  ) {
    found.push('nozzleHighFlow');
  }

  return found;
};

/**
 * The states a printer starts a print in now. The cloud asks the printer
 * whether it takes `START_PRINT`; this server has no such answer for the
 * slicer, so these four states stand for it. A printer in any other state is
 * drawn and cannot be picked.
 */
export const PRINTABLE_STATES: readonly string[] = [
  'READY',
  'IDLE',
  'FINISHED',
  'STOPPED',
];

export const canPrintNow = (printer: SlicerPrinter): boolean =>
  PRINTABLE_STATES.includes(printer.connect_state);

/** The cloud's order of states: the printers that can print first. */
const STATE_ORDER: readonly string[] = [
  'READY',
  'IDLE',
  'FINISHED',
  'STOPPED',
  'MANIPULATING',
  'ATTENTION',
  'BUSY',
  'PAUSED',
  'PRINTING',
  'PREPARED',
  'OFFLINE',
  'ERROR',
  'UNKNOWN',
];

const rank = (state: string): number => {
  const at = STATE_ORDER.indexOf(state);

  return at === -1 ? STATE_ORDER.length : at;
};

/** One row of the list: a printer and how it does not suit the file. */
export interface Candidate {
  printer: SlicerPrinter;
  problems: Incompatibility[];
  printable: boolean;
}

/**
 * The printers to offer. A printer of another model is left out, as the
 * cloud leaves it out. The rest are ordered by state, then the ones with no
 * problem first, then by name.
 */
export const candidatesFor = (
  request: PrintRequest,
  printers: readonly SlicerPrinter[],
): Candidate[] =>
  printers
    .map((printer) => ({
      printer,
      problems: incompatibilities(request, printer),
      printable: canPrintNow(printer),
    }))
    .filter(({ problems }) => !problems.includes('printerModel'))
    .sort(
      (a, b) =>
        rank(a.printer.connect_state) - rank(b.printer.connect_state) ||
        Number(a.problems.length > 0) - Number(b.problems.length > 0) ||
        (a.printer.name ?? '').localeCompare(b.printer.name ?? ''),
    );

/**
 * The printer picked before the person picks one: the one the slicer named,
 * else the only one offered, and only a printer that can print now.
 */
export const preselected = (
  request: PrintRequest,
  candidates: readonly Candidate[],
): string | undefined => {
  const named = candidates.find(
    (candidate) => candidate.printer.uuid === request.printerUuid,
  );
  if (named?.printable === true) return named.printer.uuid;
  const [only, ...rest] = candidates;
  if (only?.printable === true && rest.length === 0) return only.printer.uuid;

  return undefined;
};

/**
 * The team every file of this server belongs to. The slicer's PRINT message
 * names a team, and this server has one.
 */
export const TEAM_ID = 1;

/**
 * The message that sends the file to the printer now. This server has no
 * queue, so the print starts at once: `set_ready` and position 0, as the
 * cloud's "print now". The fields go flat and under `data`, because older
 * PrusaSlicer reads them flat.
 */
export const printMessage = (
  printerUuid: string,
  filename: string,
): SlicerMessage => {
  const data = {
    set_ready: true,
    position: 0,
    wait_until: undefined,
    filename,
    printer_uuid: printerUuid,
    team_id: TEAM_ID,
  };

  return { action: 'PRINT', ...data, data };
};
