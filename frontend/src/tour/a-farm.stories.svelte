<!--
  A walk through the product for farm of twelve printers. Each story is one stop, in the order a
  person meets them, and fixes the fleet size so the toolbar switch does not
  change it.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../test/story/app-at.svelte';
  import { notSetUp, PRINTER, signedOut } from './stops';

  const { Story } = defineMeta({ title: 'Tour/A farm' });
</script>

<Story name="Sign in">
  <p class="note">
    Every visit starts at the sign-in page. A person enters a password or uses
    the account provider that the server names.
  </p>
  <AppAt path="/" size="many" overrides={signedOut} />
</Story>

<Story name="First run">
  <p class="note">
    A new server asks for a first setup before anyone can sign in. The wizard
    names the steps that are left.
  </p>
  <AppAt path="/" size="many" overrides={notSetUp} />
</Story>

<Story name="Home">
  <p class="note">
    Home is the first page after sign-in. It shows the dashboard of the farm of
    twelve printers.
  </p>
  <AppAt path="/" size="many" />
</Story>

<Story name="Printers">
  <p class="note">
    The printers page lists all twelve printers. Each card shows the state and
    the current job.
  </p>
  <AppAt path="/printers" size="many" />
</Story>

<Story name="Printer detail">
  <p class="note">
    A click on a printer opens its page. The status tab shows the telemetry, the
    camera and the current print.
  </p>
  <AppAt path={PRINTER} size="many" />
</Story>

<Story name="Jobs">
  <p class="note">
    The jobs page lists the prints of all twelve printers, newest first.
  </p>
  <AppAt path="/jobs" size="many" />
</Story>

<Story name="Storage">
  <p class="note">
    Storage holds the files that can be sent to a printer. A person opens a
    folder to see its files.
  </p>
  <AppAt path="/storage/local" size="many" />
</Story>

<Story name="Settings">
  <p class="note">
    Settings holds the account, the access, the network and the server. The
    account page opens first.
  </p>
  <AppAt path="/settings/account" size="many" />
</Story>

<Story name="Extensions">
  <p class="note">
    Extensions add pages and cards to the product. A person turns each one on or
    off here.
  </p>
  <AppAt path="/settings/extensions" size="many" />
</Story>

<style>
  .note {
    margin: 0;
    padding: 0.75rem 1rem;
    font:
      0.875rem/1.4 system-ui,
      sans-serif;
  }
</style>
