// Send to Prusa, from the click to the portal's page. The browser builds the
// bundle through the core, uploads it to the portal itself and opens the
// portal's page for it (ADR-20261015-mde rule 16). The server never calls out.
import type { SendIntent } from '@prusa/connect-ui/shell';

/** The tab the portal's page opens in. */
export interface PortalTab {
  opener: unknown;
  location: { href: string };
  close: () => void;
}

export interface SendSteps {
  /** Opens an empty tab. Null when the browser refused one. */
  open: () => PortalTab | null;
  /** Builds the bundle on the core. */
  build: () => Promise<Blob>;
  /** The build this server runs, which the portal files the bundle under. */
  version: () => Promise<string>;
  /** Uploads the bundle and answers the page the portal made for it. */
  upload: (bundle: Blob, version: string) => Promise<{ url: string }>;
  /** Shows the portal's page here, when no tab could be opened. */
  go: (url: string) => void;
}

/** The portal's page for one upload, told what the person wants to do. */
export const portalPage = (url: string, intent: SendIntent): string => {
  const page = new URL(url);
  page.searchParams.set('intent', intent);

  return page.toString();
};

/**
 * Runs Send to Prusa. Call it inside the click: the tab opens before the
 * first await, which is what keeps a popup blocker from stopping it. A
 * failure closes the tab and is thrown for the screen to show.
 */
export const sendDiagnosticsToPortal = async (
  intent: SendIntent,
  steps: SendSteps,
): Promise<void> => {
  const tab = steps.open();
  if (tab !== null) {
    // The portal's page gets no handle back to this one.
    tab.opener = null;
  }

  try {
    const bundle = await steps.build();
    const { url } = await steps.upload(bundle, await steps.version());
    const page = portalPage(url, intent);

    if (tab === null) {
      steps.go(page);
    } else {
      tab.location.href = page;
    }
  } catch (thrown) {
    tab?.close();
    throw thrown;
  }
};
