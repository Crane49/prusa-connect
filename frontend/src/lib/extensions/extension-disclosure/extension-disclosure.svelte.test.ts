import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionDisclosure from './extension-disclosure.svelte';

describe('ExtensionDisclosure', () => {
  it('names the licence and every connection outside, with why and what it sends', async () => {
    await render(ExtensionDisclosure, {
      props: {
        license: 'MIT',
        egressKnown: true,
        egress: [
          {
            host: 'api.github.com',
            ports: [443],
            from: 'process',
            why: 'Lists the releases.',
            sends: 'The request.',
          },
          {
            option: 'manifest_url',
            ports: [443, 8443],
            from: 'ui',
            why: 'Reads the manifest.',
            sends: 'Nothing.',
          },
        ],
      },
    });

    await expect.element(page.getByText('Licence: MIT')).toBeVisible();
    await expect
      .element(
        page.getByText('From this server it reaches api.github.com, port 443.'),
      )
      .toBeVisible();
    await expect
      .element(
        page.getByText(
          'From your browser it reaches the address in its setting manifest_url, port 443, 8443.',
        ),
      )
      .toBeVisible();
    await expect
      .element(page.getByText('Lists the releases. It sends: The request.'))
      .toBeVisible();
  });

  it('says a release with no connections connects nowhere', async () => {
    await render(ExtensionDisclosure, {
      props: { license: 'MIT', egressKnown: true, egress: [] },
    });

    await expect
      .element(page.getByText('It reaches nothing outside Prusa Connect.'))
      .toBeVisible();
  });

  it('never says an undeclared release connects nowhere', async () => {
    await render(ExtensionDisclosure, {
      props: { license: undefined, egressKnown: false, egress: [] },
    });

    await expect
      .element(page.getByText('The release names no licence.'))
      .toBeVisible();
    await expect
      .element(
        page.getByText(
          'The release does not say what it reaches outside Prusa Connect.',
        ),
      )
      .toBeVisible();
    await expect
      .element(page.getByText('It reaches nothing outside Prusa Connect.'))
      .not.toBeInTheDocument();
  });
});
