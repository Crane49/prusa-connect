// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
//
// The module draws one section, under the Connection card of each printer's Printer tab: the
// address and the API key a person types into PrusaSlicer 2 for that printer.
import { defineModule } from '@prusa/connect-ui/slots';

import { holdHost } from './host';
import { PrusalinkRoute } from './views';
import './styles.css';

export default defineModule((host) => {
  holdHost(host);

  host.register.deviceInfoSection('prusalink', PrusalinkRoute);
});
