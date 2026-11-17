<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { emptyList, failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { json } from '../../test/stub';

  const { Story } = defineMeta({ title: 'Screens/Settings/People' });

  const LIST = 'GET /api/v1/auth/accounts';

  const account = (id: string, name: string, role: string) => ({
    id,
    username: id,
    display_name: name,
    created_at: '2026-09-12T13:09:57.4639178Z',
    subject: `user:${id}`,
    role,
    grants: [role],
  });
</script>

<!--
  States the screen branches on: one account, several, none, loading, failed
  and refused. Creating, editing and removing an account open dialogs and are
  skipped.
-->

<Story name="One Account"><AppAt path="/settings/people" /></Story>

<Story name="Several Accounts">
  <AppAt
    path="/settings/people"
    overrides={{
      [LIST]: () =>
        json(200, {
          items: [
            account('operator', 'Demo', 'admin'),
            account('anna', 'Anna Nováková', 'operator'),
            account('jan', 'Jan Dvořák', 'viewer'),
          ],
        }),
    }}
  />
</Story>

<Story name="Empty">
  <AppAt path="/settings/people" overrides={{ [LIST]: emptyList }} />
</Story>

<Story name="Loading">
  <AppAt path="/settings/people" overrides={{ [LIST]: pending }} />
</Story>

<Story name="Failed">
  <AppAt path="/settings/people" overrides={{ [LIST]: failed }} />
</Story>

<Story name="Refused">
  <AppAt path="/settings/people" overrides={{ [LIST]: refused }} />
</Story>
