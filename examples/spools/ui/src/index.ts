// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives (ADR-20260911-4p6 rule 18).
//
// `defineModule` is an identity function. It exists so an author gets the
// host's types at this call site without importing the host.
import { defineModule } from '@prusa/connect-ui/slots';

import { holdHost } from './host';
import { holdShelf, Shelf } from './shelf.svelte';
import { SpoolPicker } from './spool-picker';
import { spoolsOn } from './spools';
import { SpoolsPage } from './spools-page';
import './styles.css';

export default defineModule((host) => {
  // Everything the page needs is built here, from the host and from nothing
  // else. The registration is held to `contributes.pages` in module.yaml: a
  // path the manifest does not declare is refused and this module is dropped
  // whole (ADR-20260911-4p6 rule 21).
  holdHost(host);
  holdShelf(new Shelf(spoolsOn(host), host.principal.id), host.signal);

  host.register.page('spools', SpoolsPage);

  // The shelf as a list other modules draw, under the one name ui.exports
  // in module.yaml names (ADR-20261027-wvj rule 21).
  host.register.component('spool-picker', SpoolPicker);
});
