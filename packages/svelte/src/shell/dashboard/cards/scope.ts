// Which printers a card of many keeps, one rule per scope a person can pick.
import { needsPerson, type PrinterView } from '../printers';
import type { Scope } from './config';

const IN_SCOPE: Readonly<Record<Scope, (view: PrinterView) => boolean>> = {
  all: () => true,
  printing: (view) => view.state === 'PRINTING' || view.state === 'BUSY',
  attention: needsPerson,
  idle: (view) => view.state === 'IDLE' || view.state === 'READY',
  offline: (view) => view.tier === 'not-reporting',
};

/** True when a printer is one a scope keeps. */
export const inScope = (view: PrinterView, scope: Scope): boolean =>
  IN_SCOPE[scope](view);
