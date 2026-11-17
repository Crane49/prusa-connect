<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';

  const { Story } = defineMeta({ title: 'Screens/Settings/Account' });
</script>

<!--
  States the screen branches on: the signed-in account read, still loading,
  failed and refused. Only an account admin may read the accounts, so a
  refused read is what everyone else sees: the account rows are left out, as
  they are when the read fails. A password change in flight, done or refused needs a typed form and
  is skipped.
-->

<Story name="Populated"><AppAt path="/settings/account" /></Story>

<Story name="Loading">
  <AppAt
    path="/settings/account"
    overrides={{ 'GET /api/v1/auth/accounts': pending }}
  />
</Story>

<Story name="Failed">
  <AppAt
    path="/settings/account"
    overrides={{ 'GET /api/v1/auth/accounts': failed }}
  />
</Story>

<Story name="Refused">
  <AppAt
    path="/settings/account"
    overrides={{ 'GET /api/v1/auth/accounts': refused }}
  />
</Story>
