// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives (ADR-20260911-4p6 rule 18).
//
// `defineModule` is an identity function. It exists so an author gets the
// host's types at this call site without importing the host.
import { defineModule } from '@prusa/connect-ui/slots';

import { holdHost } from './host';
import { markChanged } from './mark-changed';
import { NozzleLine } from './nozzle-line';
import { NozzleSection } from './nozzle-section';
import { NozzleTab } from './nozzle-tab';
import './styles.css';

export default defineModule((host) => {
  // Every component reads the fleet, and the fleet is on the host. It is
  // held here rather than threaded down, because the host arrives before any
  // component of this module is mounted, so there is no component for
  // `setContext` to hang on.
  holdHost(host);

  // The four, and only the four, the manifest declares. An id the
  // `contributes` block does not carry is refused and this module is dropped
  // whole (ADR-20260911-4p6 rule 21).
  host.register.deviceCard('nozzle', NozzleLine);
  host.register.deviceSection('nozzle', NozzleSection);
  host.register.deviceTab('nozzle', NozzleTab);
  host.register.deviceAction('mark-changed', markChanged(host));
});
