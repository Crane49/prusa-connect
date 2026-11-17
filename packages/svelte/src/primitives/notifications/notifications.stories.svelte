<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    LONG_NOTIFICATION_FIXTURE,
    NOTIFICATION_FIXTURE,
  } from '../../test/notification-fixture';
  import type { NotificationItem } from './types';
  import NotificationsLive from '../../test/notifications-live.svelte';
  import Notifications from './notifications.svelte';

  const ALL_READ = NOTIFICATION_FIXTURE.map((item) => ({
    ...item,
    read: true,
  }));

  const MODULE_SWITCHED_OFF: NotificationItem = {
    id: 'module',
    kind: 'module-switched-off',
    title: 'spools was switched off',
    body: 'It kept failing and the server switched it off',
    at: '08:20',
    read: false,
    actions: [{ id: 'open', label: 'Open the module', variant: 'secondary' }],
  };

  const { Story } = defineMeta({
    title: 'Primitives/Notifications',
    component: Notifications,
    args: { items: [] },
  });
</script>

<Story name="Default" asChild>
  <NotificationsLive />
</Story>

<Story name="Empty" asChild>
  <NotificationsLive initial={[]} />
</Story>

<Story name="All Read" asChild>
  <NotificationsLive initial={ALL_READ} />
</Story>

<!-- Sixteen items: the unread and earlier split, and a list that scrolls. -->
<Story name="Long List" asChild>
  <NotificationsLive initial={LONG_NOTIFICATION_FIXTURE} />
</Story>

<!-- A bottom sheet, the same overlay pattern as the phone menu. -->
<Story name="Mobile Sheet" asChild>
  <NotificationsLive presentation="sheet" initial={LONG_NOTIFICATION_FIXTURE} />
</Story>

<!-- Printers waiting right now, above the history of what already happened. -->
<Story name="Printers Waiting" asChild>
  <NotificationsLive needsAttention={5} />
</Story>

<!-- A module the server stopped restarting after repeated failures. -->
<Story name="Module Switched Off" asChild>
  <NotificationsLive initial={[MODULE_SWITCHED_OFF, ...NOTIFICATION_FIXTURE]} />
</Story>

<Story
  name="Tone Is Said Without Spending A Line"
  tags={['!dev', '!autodocs']}
  asChild
>
  <NotificationsLive />
</Story>

<Story
  name="Waiting Printers Lead The Panel"
  tags={['!dev', '!autodocs']}
  asChild
>
  <NotificationsLive needsAttention={5} />
</Story>

<Story name="What Needs Doing Sorts First" tags={['!dev', '!autodocs']} asChild>
  <NotificationsLive />
</Story>

<Story
  name="Scrolls And Splits Unread From Earlier"
  tags={['!dev', '!autodocs']}
  asChild
>
  <NotificationsLive initial={LONG_NOTIFICATION_FIXTURE} />
</Story>

<Story name="An Empty Bell Says So" tags={['!dev', '!autodocs']} asChild>
  <NotificationsLive initial={[]} />
</Story>

<Story
  name="Mobile Sheet Opens As Overlay"
  tags={['!dev', '!autodocs']}
  asChild
>
  <NotificationsLive presentation="sheet" initial={LONG_NOTIFICATION_FIXTURE} />
</Story>
