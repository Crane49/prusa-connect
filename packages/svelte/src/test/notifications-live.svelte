<!--
  The caller a `Notifications` needs. Read state outlives any one screen, so
  the component holds none of it, and a story and a test both need something
  that does.
-->
<script lang="ts">
  import {
    type NotificationItem,
    Notifications,
  } from '../primitives/notifications';
  import { NOTIFICATION_FIXTURE } from './notification-fixture';

  let {
    initial = NOTIFICATION_FIXTURE,
    presentation = 'popover',
    needsAttention,
  }: {
    initial?: NotificationItem[];
    presentation?: 'popover' | 'sheet';
    needsAttention?: number;
  } = $props();

  // svelte-ignore state_referenced_locally
  let items = $state(initial);
</script>

<div
  class={presentation === 'sheet'
    ? 'flex h-144 items-end justify-end p-4'
    : 'flex justify-end p-6'}
>
  <Notifications
    {items}
    {needsAttention}
    {presentation}
    triggerSize={presentation === 'sheet' ? 'icon-lg' : 'icon-md'}
    onToggleRead={(id: string) => {
      items = items.map((item) =>
        item.id === id ? { ...item, read: !item.read } : item,
      );
    }}
    onMarkAllRead={() => {
      items = items.map((item) => ({ ...item, read: true }));
    }}
    onAction={(notificationId: string, actionId: string) => {
      if (actionId !== 'ack') return;
      items = items.map((item) =>
        item.id === notificationId ? { ...item, read: true } : item,
      );
    }}
  />
</div>
