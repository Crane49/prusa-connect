<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { emptyList, failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { json } from '../../test/stub';

  const { Story } = defineMeta({
    title: 'Screens/Settings/People/Person access',
  });

  const PATH = '/settings/people/anna';
  const ACCOUNTS = 'GET /api/v1/auth/accounts';
  const DEVICES = 'GET /api/v1/devices';
  const GRANTS = 'GET /api/v1/grants/subject/user%3Aanna/direct';

  const PRINTER = '34e039fc-13fd-4f4a-b16b-92392643f63a';

  const anna = (role: string) => ({
    id: 'anna',
    username: 'anna',
    display_name: 'Anna Nováková',
    created_at: '2026-09-12T13:09:57.4639178Z',
    subject: 'user:anna',
    role,
    grants: [role],
  });

  const accounts =
    (...rows: ReturnType<typeof anna>[]) =>
    () =>
      json(200, { items: rows });

  const grants =
    (...relations: string[]) =>
    () =>
      json(200, {
        subject: 'user:anna',
        items: relations.map((relation) => ({
          object: `device:${PRINTER}`,
          relation,
          subject: 'user:anna',
        })),
      });
</script>

<!--
  States the screen branches on: a person without a grant, one with a grant on
  a printer, an administrator (who holds every printer, so the list is replaced
  by a note), the grants loading, failed and refused, the accounts loading, an
  unknown person, and a fleet without printers. Switching a printer on or off
  and the choice menu write grants and are skipped.
-->

<Story name="Populated">
  <AppAt
    path={PATH}
    overrides={{
      [ACCOUNTS]: accounts(anna('viewer')),
      [GRANTS]: grants('operator'),
    }}
  />
</Story>

<Story name="No Grants">
  <AppAt
    path={PATH}
    overrides={{ [ACCOUNTS]: accounts(anna('viewer')), [GRANTS]: grants() }}
  />
</Story>

<Story name="Administrator">
  <AppAt
    path={PATH}
    overrides={{ [ACCOUNTS]: accounts(anna('admin')), [GRANTS]: grants() }}
  />
</Story>

<Story name="Loading">
  <AppAt
    path={PATH}
    overrides={{ [ACCOUNTS]: accounts(anna('viewer')), [GRANTS]: pending }}
  />
</Story>

<Story name="Failed">
  <AppAt
    path={PATH}
    overrides={{ [ACCOUNTS]: accounts(anna('viewer')), [GRANTS]: failed }}
  />
</Story>

<Story name="Refused">
  <AppAt
    path={PATH}
    overrides={{ [ACCOUNTS]: accounts(anna('viewer')), [GRANTS]: refused }}
  />
</Story>

<Story name="Loading Person">
  <AppAt path={PATH} overrides={{ [ACCOUNTS]: pending }} />
</Story>

<Story name="Unknown Person">
  <AppAt path="/settings/people/nobody" />
</Story>

<Story name="No Printers">
  <AppAt
    path={PATH}
    overrides={{
      [ACCOUNTS]: accounts(anna('viewer')),
      [GRANTS]: grants(),
      [DEVICES]: emptyList,
    }}
  />
</Story>
