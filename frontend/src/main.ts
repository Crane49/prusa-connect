import { ApiError, NetworkError } from '@prusa/connect';
import { theme } from '@prusa/connect-ui';
import { locale } from '@prusa/connect-ui/i18n';
import { slicer } from '@prusa/connect-ui/slicer';
import { mount } from 'svelte';

import App from './app.svelte';
import { startLocale } from './lib/i18n';
import { log } from './lib/log';
import { client } from './lib/client';
import { startSlicerBridge } from './lib/slicer/bridge';
import './styles/global.css';

// The language is chosen before the first screen draws, so nothing flashes in
// English and is then replaced (frontend-i18n.md rule 8).
startLocale();

// Inside PrusaSlicer the page installs the functions the slicer calls, and
// asks the slicer for its configuration. The slicer opens the page session
// and sets its cookie itself, so the page never holds a credential. Anywhere else this
// does nothing (ADR-20261112-2eb rule 4).
startSlicerBridge({
  page: window,
  slicer,
  endSession: async () => {
    try {
      await client.logout();
    } catch {
      // The page session ends on its own within the hour.
    }
  },
  reload: () => {
    window.location.reload();
  },
  onLook: ({ language, dark }) => {
    if (language !== undefined) locale.set(language);
    if (dark !== undefined) theme.follow(dark ? 'dark' : 'light');
  },
});

const target = document.getElementById('app');
if (target === null) throw new Error('the page has no #app');

const pageLog = log.scope('page');

window.addEventListener('error', (event) => {
  pageLog.error(event.message, {
    stack: event.error instanceof Error ? event.error.stack : undefined,
  });
});
window.addEventListener('unhandledrejection', (event) => {
  const reason: unknown = event.reason;
  // The client's onFailure has already recorded these.
  if (reason instanceof ApiError || reason instanceof NetworkError) return;
  pageLog.error(reason instanceof Error ? reason.message : String(reason), {
    stack: reason instanceof Error ? reason.stack : undefined,
  });
});

mount(App, { target, props: { useServerAppearance: true } });
