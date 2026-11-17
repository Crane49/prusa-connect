<!--
  A walk through the product for one-printer desk. Each story is one stop, in the order a
  person meets them, and fixes the fleet size so the toolbar switch does not
  change it.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../test/story/app-at.svelte';
  import { notSetUp, PRINTER, signedOut } from './stops';

  const { Story } = defineMeta({ title: 'Tour/One printer' });
</script>

<Story name="Sign in">
  <p class="note">
    Every visit starts at the sign-in page. A person enters a password or uses
    the account provider that the server names.
  </p>
  <AppAt path="/" size="one" overrides={signedOut} />
</Story>

<Story name="First run">
  <p class="note">
    A new server asks for a first setup before anyone can sign in. The wizard
    names the steps that are left.
  </p>
  <AppAt path="/" size="one" overrides={notSetUp} />
</Story>

<Story name="Home">
  <p class="note">
    Home is the first page after sign-in. It shows the dashboard of the
    one-printer desk.
  </p>
  <AppAt path="/" size="one" />
</Story>

<Story name="Printers">
  <p class="note">
    The printers page lists the one printer. Each card shows the state and the
    current job.
  </p>
  <AppAt path="/printers" size="one" />
</Story>

<Story name="Printer detail">
  <p class="note">
    A click on a printer opens its page. The status tab shows the telemetry, the
    camera and the current print.
  </p>
  <AppAt path={PRINTER} size="one" />
</Story>

<Story name="Jobs">
  <p class="note">
    The jobs page lists the prints of that printer, newest first.
  </p>
  <AppAt path="/jobs" size="one" />
</Story>

<Story name="Storage">
  <p class="note">
    Storage holds the files that can be sent to a printer. A person opens a
    folder to see its files.
  </p>
  <AppAt path="/storage/local" size="one" />
</Story>

<Story name="Settings">
  <p class="note">
    Settings holds the account, the access, the network and the server. The
    account page opens first.
  </p>
  <AppAt path="/settings/account" size="one" />
</Story>

<Story name="Extensions">
  <p class="note">
    Extensions add pages and cards to the product. A person turns each one on or
    off here.
  </p>
  <AppAt path="/settings/extensions" size="one" />
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
