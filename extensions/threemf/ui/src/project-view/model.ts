// What a page draws from one 3MF project: the state of each copy, the labels
// of the beds, and the geometry of the plan. Plain functions over the
// document get-project answers, so a test reads them without a DOM.
import type { ProjectDocument } from '../api';

export type Project = ProjectDocument;
export type ProjectSetup = Project['setups'][number];
export type ProjectBed = ProjectSetup['beds'][number];
export type ProjectObject = Project['objects'][number];
export type ProjectInstance = ProjectObject['instances'][number];
export type ProjectThumbnail = Project['thumbnails'][number];
export type ProjectIssue = Project['issues'][number];
export type ProjectSettings = ProjectSetup['settings'];
export type ProjectPart = ProjectObject['parts'][number];

/** A point on the bed plane, in millimetres. */
export interface Point {
  readonly x: number;
  readonly y: number;
}

/** One bed, by its setup and its index in that setup. */
export interface BedRef {
  readonly setup: number;
  readonly bed: number;
}

/**
 * What the plan colours a copy by. `unknown` is a copy in a file whose beds
 * the server does not know, which is every copy of a file from a program it
 * has no dialect for.
 */
export type InstanceState =
  'printable' | 'nonPrintable' | 'partlyOutside' | 'outside' | 'unknown';

/** Where a copy stands against its bed, apart from whether it prints. */
export type Placement = 'inside' | 'partlyOutside' | 'outside' | 'unknown';

export const placementOf = (instance: ProjectInstance): Placement => {
  switch (instance.placement) {
    case 'partly_outside':
      return 'partlyOutside';
    case 'outside':
      return 'outside';
    case 'unknown':
      return 'unknown';
    case 'inside':
      return 'inside';
  }
};

/**
 * The one state a copy is counted under, so the legend's counts add up to
 * every copy. A copy the slicer leaves out of the print is counted as not
 * printable wherever it stands, because that is the choice its author made;
 * where it stands is still drawn, in the colour of its outline. A printable
 * copy is counted by where it stands.
 */
export const instanceState = (instance: ProjectInstance): InstanceState => {
  if (!instance.printable) return 'nonPrintable';
  const placement = placementOf(instance);

  return placement === 'inside' ? 'printable' : placement;
};

/** The program behind a project, as the producer badge tells it. */
export type ProducerKind =
  | 'prusaslicer'
  | 'prusaslicerPreview'
  | 'orcaslicer'
  | 'unsupported'
  | 'generic';

/**
 * A PrusaSlicer version with a word after the numbers, such as
 * 3.0.0-alpha12, is a preview release.
 */
const PREVIEW = /-(?:alpha|beta|rc)/i;

export const producerKind = (producer: Project['producer']): ProducerKind => {
  if (!producer.supported) return 'unsupported';

  switch (producer.dialect) {
    case 'prusaslicer2':
    case 'prusaslicer3':
      return PREVIEW.test(producer.version ?? '')
        ? 'prusaslicerPreview'
        : 'prusaslicer';
    case 'orcaslicer':
      return 'orcaslicer';
    case 'generic':
      return 'generic';
  }
};

/** The program's name and version, or the Application entry as written. */
export const producerText = (producer: Project['producer']): string => {
  const named = [producer.name, producer.version]
    .filter((part) => part !== undefined && part !== '')
    .join(' ');

  return named === '' ? (producer.application ?? '') : named;
};

/** A stable key for one bed. */
export const bedKey = (ref: BedRef): string =>
  `${String(ref.setup)}:${String(ref.bed)}`;

/** A stable key for one copy. */
export const instanceKey = (object: number, instance: number): string =>
  `${String(object)}:${String(instance)}`;

/** Every bed of every setup, setup by setup. */
export const allBeds = (
  project: Project,
): readonly { setup: ProjectSetup; bed: ProjectBed }[] =>
  project.setups.flatMap((setup) => setup.beds.map((bed) => ({ setup, bed })));

/** The bed a ref names, or undefined for a ref the project does not hold. */
export const bedOf = (project: Project, ref: BedRef): ProjectBed | undefined =>
  project.setups[ref.setup]?.beds[ref.bed];

/** The printer models the setups are for, each once, in setup order. */
export const printersOf = (project: Project): readonly string[] => [
  ...new Set(
    project.setups
      .map((setup) => setup.printer_model ?? '')
      .filter((model) => model !== ''),
  ),
];

/** The beds the copies of one object stand on, each once, in bed order. */
export const bedsOfObject = (object: ProjectObject): readonly BedRef[] => {
  const seen = new Map<string, BedRef>();
  for (const instance of object.instances) {
    if (instance.bed === undefined) continue;
    seen.set(bedKey(instance.bed), instance.bed);
  }

  return [...seen.values()].sort((a, b) => a.setup - b.setup || a.bed - b.bed);
};

/** One bed as the plan draws it, in the drawing's own coordinates. */
export interface PlanBed {
  readonly key: string;
  readonly ref: BedRef;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
  /** The bed's outline as SVG points, absent when the file gives none. */
  readonly shape: string | undefined;
  /** False for a bed whose size the file does not give. It is drawn around its copies. */
  readonly known: boolean;
  /** No copy stands on the bed. */
  readonly empty: boolean;
  /**
   * Copies stand on the bed and the slicer leaves every one of them out of
   * the print: parts parked there, not a print.
   */
  readonly parkedOnly: boolean;
}

/** One copy as the plan draws it. */
export interface PlanInstance {
  readonly key: string;
  readonly object: number;
  readonly instance: number;
  readonly state: InstanceState;
  readonly placement: Placement;
  readonly printable: boolean;
  readonly points: string;
  readonly centreX: number;
  readonly centreY: number;
  /** The widest a label inside the copy may be, in the drawing's units. */
  readonly labelWidth: number;
  /** The tallest a label inside the copy may be. */
  readonly labelHeight: number;
  /** The copy stands on no bed and is drawn in the area off the beds. */
  readonly offBed: boolean;
}

/** A box in the drawing's coordinates. */
export interface PlanArea {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/** What one plan draws. */
export interface Plan extends PlanArea {
  /** The size of a label, a fixed share of the drawing, so text reads the same at any scale. */
  readonly text: number;
  readonly beds: readonly PlanBed[];
  readonly instances: readonly PlanInstance[];
  /** The area the copies on no bed are laid out in, beside the beds. */
  readonly offBed: PlanArea | undefined;
}

interface Box {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

const emptyBox = (): Box => ({
  minX: Infinity,
  minY: Infinity,
  maxX: -Infinity,
  maxY: -Infinity,
});

const grow = (box: Box, x: number, y: number): void => {
  box.minX = Math.min(box.minX, x);
  box.minY = Math.min(box.minY, y);
  box.maxX = Math.max(box.maxX, x);
  box.maxY = Math.max(box.maxY, y);
};

const growBox = (box: Box, by: Box): void => {
  grow(box, by.minX, by.minY);
  grow(box, by.maxX, by.maxY);
};

const finite = (box: Box): boolean => Number.isFinite(box.minX);

/** The share of the drawing a label's height takes. */
const TEXT_SHARE = 0.03;
/** The margin around the drawing, as a share of its larger side. */
const MARGIN_SHARE = 0.04;
/** The smallest margin, in millimetres, so a tiny drawing still has room. */
const MIN_MARGIN = 4;
/** The room around copies that stand on a bed of unknown size. */
const UNKNOWN_BED_PAD = 10;
/** A bed of unknown size with nothing on it is drawn this large. */
const UNKNOWN_BED_SIZE = 100;
/** The room between the beds and the area off them, and around each copy in it. */
const OFF_BED_GAP = 30;
const OFF_BED_PAD = 10;

/** A copy's outline, or its box when the file gave no outline. */
const outlineOf = (instance: ProjectInstance): readonly Point[] =>
  instance.outline.length > 0
    ? instance.outline
    : [
        instance.min,
        { x: instance.min.x + instance.size.x, y: instance.min.y },
        {
          x: instance.min.x + instance.size.x,
          y: instance.min.y + instance.size.y,
        },
        { x: instance.min.x, y: instance.min.y + instance.size.y },
      ];

/** A copy's outline in the drawing's coordinates, y pointing down. */
const drawn = (
  outline: readonly Point[],
  offset: Point,
): { points: Point[]; box: Box } => {
  const box = emptyBox();
  const points = outline.map((point) => {
    const x = point.x + offset.x;
    const y = -(point.y + offset.y);
    grow(box, x, y);

    return { x, y };
  });

  return { points, box };
};

const pointsText = (points: readonly Point[], dx = 0, dy = 0): string =>
  points
    .map(
      (point) =>
        `${String(round(point.x + dx))},${String(round(point.y + dy))}`,
    )
    .join(' ');

/** What one plan draws: every bed, one bed, or only the copies on no bed. */
export type PlanFocus = BedRef | 'off' | undefined;

/**
 * The geometry of a plan, in millimetres with y pointing down the screen, so
 * the back of the bed is at the top as a slicer shows it.
 *
 * With a bed for `focus` the plan draws that one bed at the origin and the
 * copies on it. With `off` it draws only the copies on no bed. Without it the
 * plan draws every bed at its offset, which lays the beds out the way the
 * slicer did. A copy on no bed is not drawn where it stands, which may be
 * anywhere around or across the beds: it is laid out in an area of its own
 * beside them, so no bed looks occupied by it.
 */
export const planOf = (project: Project, focus?: PlanFocus): Plan => {
  const offsetOf = (ref: BedRef): Point =>
    focus === undefined
      ? (bedOf(project, ref)?.offset ?? { x: 0, y: 0 })
      : { x: 0, y: 0 };

  const onFocus = (ref: BedRef): boolean =>
    focus === undefined ||
    (focus !== 'off' && ref.setup === focus.setup && ref.bed === focus.bed);

  const instances: PlanInstance[] = [];
  const copiesBox = new Map<string, Box>();
  const occupied = new Set<string>();
  const printing = new Set<string>();
  const drawing = emptyBox();
  const homeless: {
    object: number;
    instance: number;
    copy: ProjectInstance;
  }[] = [];

  const place = (
    o: number,
    i: number,
    instance: ProjectInstance,
    points: string,
    box: Box,
    offBed: boolean,
  ): void => {
    instances.push({
      key: instanceKey(o, i),
      object: o,
      instance: i,
      state: instanceState(instance),
      placement: placementOf(instance),
      printable: instance.printable,
      points,
      centreX: round((box.minX + box.maxX) / 2),
      centreY: round((box.minY + box.maxY) / 2),
      labelWidth: round(box.maxX - box.minX),
      labelHeight: round(box.maxY - box.minY),
      offBed,
    });
  };

  project.objects.forEach((object, o) => {
    object.instances.forEach((instance, i) => {
      if (instance.bed === undefined) {
        if (focus === undefined || focus === 'off')
          homeless.push({ object: o, instance: i, copy: instance });

        return;
      }
      if (!onFocus(instance.bed)) return;
      const key = bedKey(instance.bed);
      occupied.add(key);
      if (instance.printable) printing.add(key);
      const { points, box } = drawn(
        outlineOf(instance),
        offsetOf(instance.bed),
      );
      growBox(drawing, box);
      const around = copiesBox.get(key) ?? emptyBox();
      growBox(around, box);
      copiesBox.set(key, around);
      place(o, i, instance, pointsText(points), box, false);
    });
  });

  const beds: PlanBed[] = [];
  const bedsBox = emptyBox();
  project.setups.forEach((setup, s) => {
    setup.beds.forEach((bed, b) => {
      const ref = { setup: s, bed: b };
      if (!onFocus(ref)) return;
      const offset = offsetOf(ref);
      const key = bedKey(ref);
      const known = bed.size.x > 0 && bed.size.y > 0;
      const around = copiesBox.get(key);

      let box: Box;
      if (known) {
        box = {
          minX: offset.x,
          minY: -(offset.y + bed.size.y),
          maxX: offset.x + bed.size.x,
          maxY: -offset.y,
        };
      } else if (around === undefined) {
        box = {
          minX: offset.x,
          minY: -(offset.y + UNKNOWN_BED_SIZE),
          maxX: offset.x + UNKNOWN_BED_SIZE,
          maxY: -offset.y,
        };
      } else {
        box = {
          minX: around.minX - UNKNOWN_BED_PAD,
          minY: around.minY - UNKNOWN_BED_PAD,
          maxX: around.maxX + UNKNOWN_BED_PAD,
          maxY: around.maxY + UNKNOWN_BED_PAD,
        };
      }

      const shape =
        known && setup.bed_shape.length >= 3
          ? pointsText(drawn(setup.bed_shape, offset).points)
          : undefined;

      growBox(bedsBox, box);
      growBox(drawing, box);

      beds.push({
        key,
        ref,
        x: round(box.minX),
        y: round(box.minY),
        width: round(box.maxX - box.minX),
        height: round(box.maxY - box.minY),
        shape,
        known,
        empty: !occupied.has(key),
        parkedOnly: occupied.has(key) && !printing.has(key),
      });
    });
  });

  // The copies on no bed, in columns beside the beds, each at its own size.
  let offBed: PlanArea | undefined;
  if (homeless.length > 0) {
    const top = finite(bedsBox) ? bedsBox.minY : 0;
    const height = finite(bedsBox)
      ? Math.max(bedsBox.maxY - bedsBox.minY, UNKNOWN_BED_SIZE)
      : UNKNOWN_BED_SIZE;
    const left = (finite(bedsBox) ? bedsBox.maxX : 0) + OFF_BED_GAP;
    const area = emptyBox();
    grow(area, left, top);
    let column = left + OFF_BED_PAD;
    let columnWidth = 0;
    let cursor = top + OFF_BED_PAD;

    for (const row of homeless) {
      const { points, box } = drawn(outlineOf(row.copy), { x: 0, y: 0 });
      const width = box.maxX - box.minX;
      const tall = box.maxY - box.minY;
      if (cursor + tall > top + height && cursor > top + OFF_BED_PAD) {
        column += columnWidth + OFF_BED_PAD;
        columnWidth = 0;
        cursor = top + OFF_BED_PAD;
      }
      const dx = column - box.minX;
      const dy = cursor - box.minY;
      const slot = {
        minX: column,
        minY: cursor,
        maxX: column + width,
        maxY: cursor + tall,
      };
      growBox(area, slot);
      place(
        row.object,
        row.instance,
        row.copy,
        pointsText(points, dx, dy),
        slot,
        true,
      );
      cursor += tall + OFF_BED_PAD;
      columnWidth = Math.max(columnWidth, width);
    }

    grow(
      area,
      area.maxX + OFF_BED_PAD,
      Math.max(area.maxY + OFF_BED_PAD, top + height),
    );
    growBox(drawing, area);
    offBed = {
      x: round(area.minX),
      y: round(area.minY),
      width: round(area.maxX - area.minX),
      height: round(area.maxY - area.minY),
    };
  }

  if (!finite(drawing)) {
    grow(drawing, 0, -UNKNOWN_BED_SIZE);
    grow(drawing, UNKNOWN_BED_SIZE, 0);
  }

  const side = Math.max(
    drawing.maxX - drawing.minX,
    drawing.maxY - drawing.minY,
  );
  const text = round(side * TEXT_SHARE);
  const margin = Math.max(side * MARGIN_SHARE, MIN_MARGIN);
  // The bed labels stand above the beds, so the top margin holds one line.
  const top = margin + text * 1.6;

  return {
    x: round(drawing.minX - margin),
    y: round(drawing.minY - top),
    width: round(drawing.maxX - drawing.minX + 2 * margin),
    height: round(drawing.maxY - drawing.minY + margin + top),
    text,
    beds,
    instances,
    offBed,
  };
};

/**
 * The part of a name that fits inside a copy, or nothing when the copy is too
 * small for a few letters of it. The width of a letter is near six tenths of
 * the text's height, and only the middle of an outline is sure to be inside
 * it, so the room is taken from the box's width less a margin.
 */
export const fittedLabel = (
  name: string,
  copy: Pick<PlanInstance, 'labelWidth' | 'labelHeight'>,
  size: number,
): string => {
  const LETTER = 0.6;
  const ROOM = 0.7;
  // A name cut to two or three letters says nothing, so a copy that has
  // room for fewer than six and not for the whole name carries none. The
  // list and the tip name it.
  const MIN_LETTERS = 6;
  if (name === '' || copy.labelHeight < size * 1.4) return '';
  const room = Math.floor((copy.labelWidth * ROOM) / (size * LETTER));
  if (name.length <= room) return name;
  if (room < MIN_LETTERS) return '';

  return `${name.slice(0, room - 1)}…`;
};

const round = (value: number): number => Math.round(value * 100) / 100;

export type ProjectFilament = ProjectSettings['filaments'][number];
export type ProjectLayerChange = NonNullable<
  ProjectBed['layer_changes']
>[number];
export type ProjectLayerRange = NonNullable<
  ProjectObject['layer_ranges']
>[number];

/**
 * The filaments an object's extruders are coloured by: those of the setup of
 * its first copy on a bed, or of the first setup. Only a PrusaSlicer 3
 * project has more than one setup, and its objects are shared by all.
 */
export const filamentsFor = (
  project: Project,
  object: ProjectObject | undefined,
): readonly ProjectFilament[] => {
  const setup =
    object?.instances.find((instance) => instance.bed !== undefined)?.bed
      ?.setup ?? 0;

  return project.setups[setup]?.settings.filaments ?? [];
};

/** Whether the file holds G-code for its beds and no object to slice. */
export const isGCodeOnly = (project: Project): boolean =>
  project.issues.some((issue) => issue.code === 'gcode_only');

/** The beds that carry G-code the slicer stored. */
export const bedsWithGCode = (project: Project): number =>
  allBeds(project).filter(({ bed }) => (bed.gcode ?? '') !== '').length;

/**
 * The extruder an object is drawn in: that of its first model part, else the
 * first it prints with. An object of a project with one extruder prints with
 * extruder 1.
 */
export const mainExtruder = (object: ProjectObject): number =>
  object.parts.find(
    (part) => part.type === 'model' && part.extruder !== undefined,
  )?.extruder ??
  object.extruders?.[0] ??
  1;

/**
 * The colour of the filament an object is drawn in, as the file writes it, or
 * undefined for a file that names no colour.
 */
export const objectColour = (
  project: Project,
  object: ProjectObject,
): string | undefined => {
  const colour = filamentsFor(project, object)[mainExtruder(object) - 1]
    ?.colour;

  return colour === undefined || colour === '' ? undefined : colour;
};

/**
 * Whether a copy can be ticked to print: it stands on a bed and not partly
 * off it. A copy left out of the print can be ticked, because printing a
 * selection prints exactly the ticked copies.
 */
export const isSelectable = (instance: ProjectInstance): boolean =>
  instance.bed !== undefined &&
  instance.placement !== 'partly_outside' &&
  instance.placement !== 'outside';

/** The picture that shows the whole project: one of no bed, else the first. */
export const projectPicture = (project: Project): number | undefined =>
  (
    project.thumbnails.find((picture) => picture.bed === undefined) ??
    project.thumbnails[0]
  )?.index;

/** One copy, by its object and its index in that object. */
export interface CopyRef {
  readonly object: number;
  readonly instance: number;
}

/** The copies a view of the plan shows: on one bed, on no bed, or all. */
export const copiesIn = (
  project: Project,
  focus: PlanFocus,
): readonly CopyRef[] =>
  project.objects.flatMap((object, o) =>
    object.instances.flatMap((instance, i): CopyRef[] => {
      if (focus === undefined) return [{ object: o, instance: i }];
      if (focus === 'off')
        return instance.bed === undefined ? [{ object: o, instance: i }] : [];
      return instance.bed?.setup === focus.setup &&
        instance.bed.bed === focus.bed
        ? [{ object: o, instance: i }]
        : [];
    }),
  );

/** The layer height and the nozzles of the first setup, the facts a person checks first. */
export const headlineSettings = (
  project: Project,
): { layer: number | undefined; nozzles: readonly number[] } => {
  const setup = project.setups[0];

  return {
    layer: setup?.settings.layer_height_mm,
    nozzles: [
      ...new Set(setup?.nozzles.map((nozzle) => nozzle.diameter_mm) ?? []),
    ],
  };
};

/** Copies grouped by their object, the objects in the order first met. */
export const byObject = (
  copies: readonly CopyRef[],
): readonly { object: number; copies: readonly CopyRef[] }[] => {
  const groups = new Map<number, CopyRef[]>();
  for (const copy of copies) {
    const group = groups.get(copy.object) ?? [];
    group.push(copy);
    groups.set(copy.object, group);
  }

  return [...groups.entries()].map(([object, group]) => ({
    object,
    copies: group,
  }));
};
