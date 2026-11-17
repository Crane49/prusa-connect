// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
import { defineModule } from '@prusa/connect-ui/slots';

import { apiOn } from './api';
import { holdSession } from './session';
import { StatusStore } from './status.svelte';
import { DeviceLogs, BuddyDebugLogsPage } from './views';
import './styles.css';

export default defineModule((host) => {
  // Everything the views need is built here, from the host and from nothing
  // else. The registrations are held to `contributes` in module.yaml.
  const api = apiOn(host);
  const status = new StatusStore(api);

  host.signal.addEventListener(
    'abort',
    () => {
      status.dispose();
    },
    { once: true },
  );

  holdSession({ host, api, status });

  host.register.page('buddy-debug-logs', BuddyDebugLogsPage);
  host.register.deviceTab('logs', DeviceLogs);
});
