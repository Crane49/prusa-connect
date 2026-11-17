// The words the project view draws for a bed, a copy and a size. Each reads
// the catalogue, so a change of language rewrites it where a `$derived` reads
// it.
import { formatNumber, formatUnit, t } from '../i18n';
import {
  type BedRef,
  bedOf,
  type Placement,
  type Project,
  type ProjectInstance,
} from './model';

/** The catalogue key of each placement's name. */
export const PLACEMENT_KEY: Readonly<Record<Placement, string>> = {
  inside: 'projectView.placement.inside',
  partlyOutside: 'projectView.state.partlyOutside',
  outside: 'projectView.state.outside',
  unknown: 'projectView.state.unknown',
};

/**
 * A bed as a slicer names it: its number from 1, the name a person gave an
 * OrcaSlicer plate, and the printer when the project has more than one setup.
 */
export const bedLabel = (project: Project, ref: BedRef): string => {
  const bed = bedOf(project, ref);
  const number = formatNumber(ref.bed + 1);
  const named =
    bed?.name !== undefined && bed.name !== ''
      ? t('projectView.bed.named', { number, name: bed.name })
      : t('projectView.bed.name', { number });

  const printer = project.setups[ref.setup]?.printer_model;
  if (project.setups.length < 2 || printer === undefined || printer === '') {
    return named;
  }

  return t('projectView.bed.withPrinter', { printer, number });
};

/** An object's name, or the words for an object with none. */
export const objectName = (name: string | undefined): string =>
  name === undefined || name === '' ? t('projectView.objects.unnamed') : name;

/** A length in millimetres, to a tenth. */
export const millimetres = (value: number): string =>
  formatUnit(value, 'mm', { maximumFractionDigits: 1 });

/** A copy's size: width, depth and height. */
export const copySize = (instance: ProjectInstance): string =>
  t('projectView.copy.size', {
    width: millimetres(instance.size.x),
    depth: millimetres(instance.size.y),
    height: millimetres(instance.size.z),
  });

/** A copy as a person reads it: the object's name, and which copy of how many. */
export const copyLabel = (project: Project, object: number, index: number) =>
  t('projectView.copy.label', {
    name: objectName(project.objects[object]?.name),
    number: formatNumber(index + 1),
    count: formatNumber(project.objects[object]?.instances.length ?? 0),
  });
