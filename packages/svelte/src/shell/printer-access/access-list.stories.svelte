<!--
  The card and the row both access screens draw, on their own. The rows are
  written out rather than derived from grants, so each state shows once.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { AccessOption } from '../../domain';
  import AccessList from './access-list.svelte';
  import AccessRowView from './access-row.svelte';
  import { PRINTER } from './access.fixtures';
  import type { AccessRow } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Access List',
    component: AccessList,
  });

  const OPTIONS: readonly AccessOption[] = [
    { value: 'org', blocked: undefined },
    { value: 'viewer', blocked: 'inOrg' },
    { value: 'operator', blocked: undefined },
    { value: 'manager', blocked: 'aboveCaller' },
  ];

  const row = (
    subject: string,
    state: AccessRow['state'],
    extra: Partial<AccessRow> = {},
  ): AccessRow => ({
    key: subject,
    deviceId: PRINTER,
    subject: `user:${subject}`,
    primary: subject,
    mono: true,
    details: ['Viewer in the organization'],
    name: subject,
    orgRole: 'viewer',
    state,
    busy: false,
    ...extra,
  });

  const editable = (
    on: boolean,
    extra: { inert?: boolean; switchBlocked?: 'aboveCaller' } = {},
  ): AccessRow['state'] => ({
    kind: 'editable',
    on,
    choice: 'operator',
    inert: extra.inert ?? false,
    options: OPTIONS,
    switchBlocked: extra.switchBlocked,
  });

  const rows: readonly AccessRow[] = [
    row('jana', { kind: 'locked', lock: 'orgAdmin' }, { orgRole: 'admin' }),
    row('you', { kind: 'locked', lock: 'self' }),
    row('intern', editable(true)),
    row('workshop', editable(false)),
  ];

  const ignore = (): void => undefined;
  const EMPTY = {
    heading: 'Nobody else is on this server',
    note: 'Add people on the People page.',
  };
</script>

<Story name="With Rows" asChild>
  <AccessList
    title="Access"
    description="Who can use this printer."
    {rows}
    waiting={false}
    fault={undefined}
    search=""
    onSearch={ignore}
    searchLabel="Search people"
    empty={EMPTY}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Empty" asChild>
  <AccessList
    title="Access"
    description="Who can use this printer."
    rows={[]}
    waiting={false}
    fault={undefined}
    search=""
    onSearch={ignore}
    searchLabel="Search people"
    empty={EMPTY}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="No Match" asChild>
  <AccessList
    title="Access"
    description="Who can use this printer."
    rows={[]}
    waiting={false}
    fault={undefined}
    search="nobody"
    onSearch={ignore}
    searchLabel="Search people"
    empty={EMPTY}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Editable" asChild>
  <AccessRowView
    row={row('intern', editable(true))}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Switched Off" asChild>
  <AccessRowView
    row={row('workshop', editable(false))}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Grant Changes Nothing" asChild>
  <AccessRowView
    row={row('intern', editable(true, { inert: true }))}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Above The Caller" asChild>
  <AccessRowView
    row={row('intern', editable(true, { switchBlocked: 'aboveCaller' }))}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Busy" asChild>
  <AccessRowView
    row={row('intern', editable(true), { busy: true })}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Organization Administrator" asChild>
  <AccessRowView
    row={row(
      'jana',
      { kind: 'locked', lock: 'orgAdmin' },
      { orgRole: 'admin' },
    )}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>

<Story name="Row Owner" asChild>
  <AccessRowView
    row={row('owner', { kind: 'locked', lock: 'owner' })}
    onOn={ignore}
    onChoice={ignore}
  />
</Story>
