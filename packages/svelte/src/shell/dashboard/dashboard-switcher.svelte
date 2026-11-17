<!--
  The name of the dashboard on screen, as the page's heading, which opens the
  list of every dashboard and, for a person who may write, the way to a new
  one.

  A long name is cut short with an ellipsis, and the button's title holds the
  whole of it.

  The trigger is a heading-sized button of its own. The library's button is
  sized for a toolbar, and its type size and height are not a caller's to
  change.
-->
<script lang="ts">
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
  import HouseIcon from '@lucide/svelte/icons/house';
  import PlusIcon from '@lucide/svelte/icons/plus';

  import { t } from '../../i18n';
  import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuRadioGroup,
    DropdownMenuRadioItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
    TitleButton,
  } from '../../primitives';
  import { dashboardContext } from './context';

  let {
    id,
    name,
    onNew,
  }: {
    /** The dashboard's id, or undefined for one written from the fleet. */
    id: number | undefined;
    name: string;
    onNew: () => void;
  } = $props();

  const { dashboards, navigation } = dashboardContext();
</script>

<DropdownMenu>
  <DropdownMenuTrigger>
    {#snippet child({ props })}
      <h1 class="-ms-2 min-w-0">
        <TitleButton
          {...props}
          icon={ChevronsUpDownIcon}
          data-part="dashboard-switcher"
          label={t('dashboard.switcher.open', { name })}
          title={name}
        >
          {name}
        </TitleButton>
      </h1>
    {/snippet}
  </DropdownMenuTrigger>
  <DropdownMenuContent align="start" class="min-w-56">
    <DropdownMenuLabel>{t('dashboard.switcher.label')}</DropdownMenuLabel>
    <DropdownMenuRadioGroup
      value={id === undefined ? '' : String(id)}
      onValueChange={(next: string) => {
        navigation.open(Number(next));
      }}
    >
      {#each dashboards.rows as row (row.id)}
        <DropdownMenuRadioItem value={String(row.id)}>
          <span class="grid min-w-0">
            <span class="flex items-center gap-2 truncate">
              {row.name}
              {#if row.id === dashboards.defaultId}
                <HouseIcon
                  aria-label={t('dashboard.switcher.home')}
                  class="size-3.5 shrink-0 text-muted-foreground"
                />
              {/if}
            </span>
            <span class="text-xs text-muted-foreground">
              {t('dashboard.switcher.cards', { count: row.cards })}
            </span>
          </span>
        </DropdownMenuRadioItem>
      {/each}
    </DropdownMenuRadioGroup>
    {#if dashboards.rights.write}
      <DropdownMenuSeparator />
      <DropdownMenuItem onSelect={onNew} data-part="dashboard-new">
        <PlusIcon aria-hidden="true" />
        {t('dashboard.action.new')}
      </DropdownMenuItem>
    {/if}
  </DropdownMenuContent>
</DropdownMenu>
