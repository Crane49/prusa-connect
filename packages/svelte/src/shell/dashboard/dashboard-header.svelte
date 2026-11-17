<!--
  The head of a dashboard while it is being looked at: its name, which opens
  the list of every dashboard (dashboard-switcher.svelte), who sees it when that is not everybody, and
  what a person can do with it.

  A person who may only look sees the name, the list, the link and the wall,
  and no control that changes a dashboard. A person who may configure also
  sees the control that makes this dashboard the home page (ADR-20261008-sww rule 11).
  The dashboard written from the fleet has no id yet, so it offers Edit, which
  saves it first, and nothing that needs one.
-->
<script lang="ts">
  import CopyIcon from '@lucide/svelte/icons/copy';
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
  import HouseIcon from '@lucide/svelte/icons/house';
  import MonitorIcon from '@lucide/svelte/icons/monitor';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import Share2Icon from '@lucide/svelte/icons/share-2';
  import TrashIcon from '@lucide/svelte/icons/trash';

  import type { DashboardVisibility } from '../../domain';
  import { t } from '../../i18n';
  import {
    Badge,
    Button,
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
  } from '../../primitives';
  import { dashboardContext } from './context';
  import DashboardSwitcher from './dashboard-switcher.svelte';

  let {
    id,
    name,
    visibility,
    onEdit,
    onNew,
    onShare,
    onDuplicate,
    onDelete,
  }: {
    /** The dashboard's id, or undefined for one written from the fleet. */
    id: number | undefined;
    name: string;
    visibility: DashboardVisibility;
    onEdit: () => void;
    onNew: () => void;
    onShare: () => void;
    onDuplicate: () => void;
    onDelete: () => void;
  } = $props();

  const { dashboards, navigation } = dashboardContext();

  const rights = $derived(dashboards.rights);
  const isDefault = $derived(id !== undefined && dashboards.defaultId === id);
  // The home page shows only a dashboard everybody sees (ADR-20261008-sww rule 11).
  const mayBeDefault = $derived(
    rights.configure &&
      id !== undefined &&
      !isDefault &&
      visibility === 'everyone',
  );

  const VISIBILITY_KEY: Readonly<Record<DashboardVisibility, string>> = {
    everyone: 'dashboard.visibility.everyone',
    operators: 'dashboard.visibility.operators',
    admins: 'dashboard.visibility.admins',
  };
</script>

<header
  data-part="dashboard-header"
  class="flex flex-wrap items-center justify-between gap-3"
>
  <div class="flex min-w-0 flex-1 basis-80 items-center gap-2">
    <DashboardSwitcher {id} {name} {onNew} />
    {#if visibility !== 'everyone'}
      <Badge variant="secondary" class="shrink-0">
        {t(VISIBILITY_KEY[visibility])}
      </Badge>
    {/if}
  </div>

  <div class="flex flex-wrap items-center gap-2">
    {#if rights.write}
      <Button variant="secondary" data-part="dashboard-edit" onclick={onEdit}>
        <PencilIcon aria-hidden="true" />
        {t('dashboard.action.edit')}
      </Button>
    {/if}
    {#if id !== undefined}
      <Button variant="ghost" data-part="dashboard-share" onclick={onShare}>
        <Share2Icon aria-hidden="true" />
        {t('dashboard.action.share')}
      </Button>
    {/if}
    <Button
      variant="ghost"
      data-part="dashboard-kiosk"
      onclick={() => {
        navigation.kiosk(true);
      }}
    >
      <MonitorIcon aria-hidden="true" />
      {t('dashboard.action.kiosk')}
    </Button>
    {#if rights.write && id !== undefined}
      <DropdownMenu>
        <DropdownMenuTrigger>
          {#snippet child({ props })}
            <Button
              {...props}
              variant="ghost"
              size="icon-md"
              label={t('dashboard.action.more')}
              data-part="dashboard-more"
            >
              <EllipsisIcon aria-hidden="true" />
            </Button>
          {/snippet}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {#if mayBeDefault}
            <DropdownMenuItem
              onSelect={() => {
                void dashboards.makeDefault(id);
              }}
              disabled={dashboards.busy}
              data-part="dashboard-make-default"
            >
              <HouseIcon aria-hidden="true" />
              {t('dashboard.action.makeDefault')}
            </DropdownMenuItem>
          {/if}
          <DropdownMenuItem onSelect={onDuplicate} disabled={dashboards.busy}>
            <CopyIcon aria-hidden="true" />
            {t('dashboard.action.duplicate')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onSelect={onDelete}
            data-part="dashboard-delete"
          >
            <TrashIcon aria-hidden="true" />
            {t('dashboard.action.delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    {/if}
  </div>
</header>
