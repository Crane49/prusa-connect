import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { DiscoveryVisibility, TimeServerSwitch } from '../../data';
import type {
  DiscoveryClient,
  TimeServerClient,
  TimeServerState,
  Visibility,
} from '../../data';
import { StubApiError } from '../../data/stubs';
import { expectNoViolations } from '../../test/axe';
import SettingsNetworkScreen from './settings-network-screen.svelte';

const refuse = (): never => {
  throw new Error('this fake lists nobody');
};

interface Server {
  answer: Visibility;
  written: boolean[];
  refusal: StubApiError | undefined;
}

/** The real store over a fake client, so the room is tested with what ships. */
const storeOver = (
  start: Visibility,
): { store: DiscoveryVisibility; server: Server } => {
  const server: Server = { answer: start, written: [], refusal: undefined };
  const client: DiscoveryClient = {
    listNearbyPrinters: refuse,
    getDiscoveryVisibility: () => Promise.resolve(server.answer),
    setDiscoveryVisibility: (request) => {
      server.written.push(request.body.hidden);
      if (server.refusal) return Promise.reject(server.refusal);

      server.answer = {
        ...server.answer,
        hidden: request.body.hidden,
        announcement: request.body.hidden ? 'hidden' : 'announcing',
      };

      return Promise.resolve(server.answer);
    },
  };

  return { store: new DiscoveryVisibility(client), server };
};

const VISIBLE: Visibility = {
  hidden: false,
  announcement: 'announcing',
  name: 'Workshop',
  host: 'workshop-server.local',
  port: 8080,
  services: ['_prusalink._tcp', '_prusa-connect._tcp'],
  addresses: ['192.168.1.10'],
  bridged: false,
};

describe('SettingsNetworkScreen', () => {
  it('waits for the server before it draws a switch', async () => {
    const { store } = storeOver(VISIBLE);

    await render(SettingsNetworkScreen, { props: { source: store } });

    await expect
      .element(page.getByLabelText('Reading the network setting'))
      .toHaveAttribute('aria-busy', 'true');
    await expect.element(page.getByRole('switch')).not.toBeInTheDocument();
  });

  it('hides the server when the switch is turned on', async () => {
    const { store, server } = storeOver(VISIBLE);
    await store.load();

    await render(SettingsNetworkScreen, { props: { source: store } });

    const control = page.getByRole('switch', { name: 'Hide this server' });
    await expect.element(control).toHaveAttribute('aria-checked', 'false');
    await expect
      .element(
        page.getByText(
          'Slicers and apps on this network can find this server.',
        ),
      )
      .toBeVisible();

    await userEvent.click(control);

    await expect.element(control).toHaveAttribute('aria-checked', 'true');
    await expect
      .element(page.getByText(/This server announces nothing/))
      .toBeVisible();
    expect(server.written).toEqual([true]);
  });

  // A viewer may read the setting and not change it.
  it('keeps the switch where the server has it when a change is refused', async () => {
    const { store, server } = storeOver(VISIBLE);
    await store.load();
    server.refusal = new StubApiError('core.auth/ACCESS_DENIED', 403);

    await render(SettingsNetworkScreen, { props: { source: store } });

    const control = page.getByRole('switch', { name: 'Hide this server' });
    await userEvent.click(control);

    await expect.element(page.getByRole('alert')).toBeVisible();
    await expect.element(control).toHaveAttribute('aria-checked', 'false');
    expect(store.hidden).toBe(false);
  });

  it('says a switched-off announcer makes the setting change nothing', async () => {
    const { store } = storeOver({
      hidden: false,
      announcement: 'off',
      bridged: false,
    });
    await store.load();

    await render(SettingsNetworkScreen, { props: { source: store } });

    await expect
      .element(page.getByText(/Announcing is switched off/))
      .toBeVisible();
    await expect
      .element(page.getByText('What this server announces'))
      .not.toBeInTheDocument();
  });

  // The names stay on the page while the server is hidden, because they are
  // what showing it again announces.
  it('names what the server announces, hidden or not', async () => {
    const { store } = storeOver({
      ...VISIBLE,
      hidden: true,
      announcement: 'hidden',
    });
    await store.load();

    await render(SettingsNetworkScreen, { props: { source: store } });

    await expect
      .element(page.getByText('workshop-server.local:8080'))
      .toBeVisible();
    await expect.element(page.getByText('192.168.1.10')).toBeVisible();
    await expect
      .element(page.getByText('_prusalink._tcp, _prusa-connect._tcp'))
      .toBeVisible();
  });

  it('wraps many addresses inside the page instead of cutting them to one line', async () => {
    const addresses = Array.from(
      { length: 40 },
      (_, i) => `fe80::1c2b:3dff:fe4e:5f${String(i)}%veth1a2b3c4`,
    );
    const { store } = storeOver({ ...VISIBLE, addresses });
    await store.load();

    await render(SettingsNetworkScreen, {
      props: { source: store },
    });

    const cell = document.querySelector<HTMLElement>(
      '[data-slot="key-value-row"]:nth-child(3) [data-slot="key-value-value"]',
    );
    if (cell === null) throw new Error('no addresses cell');
    expect(cell.scrollWidth).toBeLessThanOrEqual(cell.clientWidth);
    expect(cell.textContent.split('\n')).toEqual(addresses);
  });

  it('warns when the announcement stays inside a bridged container', async () => {
    const { store } = storeOver({ ...VISIBLE, bridged: true });
    await store.load();

    await render(SettingsNetworkScreen, { props: { source: store } });

    await expect.element(page.getByText(/network_mode: host/)).toBeVisible();
    await expect
      .element(page.getByText(/can find this server/))
      .not.toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { store } = storeOver({
      ...VISIBLE,
      hidden: true,
      announcement: 'hidden',
    });
    await store.load();

    const { container } = await render(SettingsNetworkScreen, {
      props: { source: store },
    });

    await expect
      .element(page.getByRole('switch'))
      .toHaveAttribute('aria-checked', 'true');
    await expectNoViolations(container);
  });

  describe('the time server', () => {
    const timeOver = (
      start: TimeServerState,
    ): { store: TimeServerSwitch; written: boolean[] } => {
      const written: boolean[] = [];
      let answer = start;
      const client: TimeServerClient = {
        getTimeServer: () => Promise.resolve(answer),
        setTimeServer: (request) => {
          written.push(request.body.enabled);
          answer = {
            enabled: request.body.enabled,
            state: request.body.enabled ? 'serving' : 'off',
            port: 123,
          };

          return Promise.resolve(answer);
        },
      };

      return { store: new TimeServerSwitch(client), written };
    };

    it('starts the time server when its switch is turned on', async () => {
      const { store: source } = storeOver(VISIBLE);
      await source.load();
      const { store, written } = timeOver({
        enabled: false,
        state: 'off',
        port: 123,
      });
      await store.load();

      await render(SettingsNetworkScreen, {
        props: { source, timeServer: store },
      });

      const toggle = page.getByRole('switch', {
        name: 'Answer the time on this network',
      });
      await expect.element(toggle).not.toBeChecked();

      await userEvent.click(toggle);

      expect(written).toEqual([true]);
      await expect.element(toggle).toBeChecked();
      await expect
        .element(page.getByText('on UDP port 123', { exact: false }))
        .toBeInTheDocument();
    });

    it('says why the time server is not answering', async () => {
      const { store: source } = storeOver(VISIBLE);
      await source.load();
      const { store } = timeOver({
        enabled: true,
        state: 'failed',
        reason: 'listen udp :123: bind: permission denied',
        port: 123,
      });
      await store.load();

      const screen = await render(SettingsNetworkScreen, {
        props: { source, timeServer: store },
      });

      await expect
        .element(page.getByText('bind: permission denied', { exact: false }))
        .toBeInTheDocument();
      await expectNoViolations(screen.container);
    });

    it('says how to give the server the right when it may not open the port', async () => {
      const { store: source } = storeOver(VISIBLE);
      await source.load();
      const { store } = timeOver({
        enabled: true,
        state: 'failed',
        reason: 'listen udp4 :123: bind: permission denied',
        cause: 'permission',
        port: 123,
      });
      await store.load();

      await render(SettingsNetworkScreen, {
        props: { source, timeServer: store, docsHref: 'https://docs.example/' },
      });

      await expect
        .element(page.getByText('may not open UDP port 123', { exact: false }))
        .toBeInTheDocument();
      await expect
        .element(page.getByRole('link', { name: 'Instructions for Linux' }))
        .toHaveAttribute(
          'href',
          'https://docs.example/install-from-a-release/#let-the-time-server-open-its-port',
        );
    });

    it('names the other program when the port is in use', async () => {
      const { store: source } = storeOver(VISIBLE);
      await source.load();
      const { store } = timeOver({
        enabled: true,
        state: 'failed',
        reason: 'listen udp4 :123: bind: address already in use',
        cause: 'in_use',
        port: 123,
      });
      await store.load();

      await render(SettingsNetworkScreen, {
        props: { source, timeServer: store, docsHref: 'https://docs.example/' },
      });

      await expect
        .element(
          page.getByText('Another program on this computer', { exact: false }),
        )
        .toBeInTheDocument();
      await expect.element(page.getByRole('link')).not.toBeInTheDocument();
    });

    it('draws no time server section without a source', async () => {
      const { store: source } = storeOver(VISIBLE);
      await source.load();

      await render(SettingsNetworkScreen, { props: { source } });

      await expect
        .element(page.getByText('Time server'))
        .not.toBeInTheDocument();
    });
  });
});
