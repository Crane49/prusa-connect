<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import PanelLeftIcon from '@lucide/svelte/icons/panel-left';

  import { untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';

  import { t } from '../../i18n';
  import {
    Button,
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuTrigger,
    Separator,
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  } from '../../primitives';
  import {
    isCurrent,
    isCurrentRow,
    type RailNavItem,
    type RailProps,
    sectionEntries,
  } from './types';

  let {
    items,
    groups = [],
    currentPath,
    collapsed = false,
    onToggle,
    settings,
    footer,
  }: RailProps = $props();

  const ungrouped = $derived(items.filter((item) => item.group === undefined));

  const grouped = $derived(
    groups
      .map((group) => ({
        group,
        items: items.filter((item) => item.group === group.id),
      }))
      .filter((run) => run.items.length > 0),
  );

  const openSections = new SvelteSet<string>();
  // A tooltip opened over an open menu pauses the menu's focus scope in
  // bits-ui, so focus and Escape never reach the menu, and as the topmost
  // dismissable layer it swallows the outside pointerdown that should close
  // it. Every rail tooltip stays shut while a menu is showing.
  const openMenus = new SvelteSet<string>();

  // A menu belongs to the page it opened on: arriving elsewhere closes it.
  let menusPath = untrack(() => currentPath);
  $effect(() => {
    if (currentPath === menusPath) return;
    menusPath = currentPath;
    untrack(() => openMenus.clear());
  });

  const sectioned = $derived(
    [...items, ...(settings === undefined ? [] : [settings])].filter(
      (item) => (item.children?.length ?? 0) > 0,
    ),
  );

  // A group holding the open page opens itself, so arriving at a section —
  // by a link, a reload or the group's own row — shows where in the group
  // the reader is. The chevron still closes it; only a move to another
  // section opens it again.
  $effect(() => {
    for (const item of sectioned) {
      const holds =
        isCurrent(item.href, currentPath) ||
        (item.children ?? []).some((child) =>
          isCurrent(child.href, currentPath),
        );
      if (holds) openSections.add(item.id);
    }
  });

  const rowClass = (current: boolean) => [
    'flex min-h-9 items-center gap-2.5 rounded-sm border px-2.5 py-1.5 text-sm font-medium tracking-wide focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
    collapsed && 'justify-center px-0',
    current
      ? 'border-selected bg-selected text-selected-foreground hover:border-selected-hovered hover:bg-selected-hovered'
      : 'border-transparent text-muted-foreground hover:bg-accent hover:text-foreground',
  ];

  const toggleSection = (id: string) => {
    if (openSections.has(id)) openSections.delete(id);
    else openSections.add(id);
  };
</script>

{#snippet navLink(
  item: RailNavItem,
  current: boolean,
  props: Record<string, unknown>,
)}
  {@const Icon = item.icon}
  <a
    {...props}
    href={item.href}
    aria-current={current ? 'page' : undefined}
    class={[...rowClass(current), item.nested && !collapsed && 'ps-7']}
  >
    {#if Icon}
      <Icon aria-hidden="true" class="size-4.5 shrink-0" />
    {/if}
    <span class={collapsed ? 'sr-only' : 'truncate'}>{item.label}</span>
  </a>
{/snippet}

<!-- A collapsed rail has no room to list the children under their item, so
     the item's row opens them in a menu beside the rail. -->
{#snippet sectionMenu(item: RailNavItem)}
  {@const Icon = item.icon}
  {@const entries = sectionEntries(item)}
  <DropdownMenu
    open={openMenus.has(item.id)}
    onOpenChange={(open) =>
      open ? openMenus.add(item.id) : openMenus.delete(item.id)}
  >
    <Tooltip disabled={openMenus.size > 0}>
      <TooltipTrigger>
        {#snippet child({ props: tip }: { props: Record<string, unknown> })}
          <DropdownMenuTrigger {...tip}>
            {#snippet child({ props })}
              <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- standing exception (ui-integration.md): a menu trigger in the rail's own Tooltip that must match the sibling nav-link rows -->
              <button
                {...props}
                type="button"
                class={[
                  ...rowClass(
                    entries.some((entry) => isCurrent(entry.href, currentPath)),
                  ),
                  'w-full',
                ]}
              >
                {#if Icon}
                  <Icon aria-hidden="true" class="size-4.5 shrink-0" />
                {/if}
                <span class="sr-only">{item.label}</span>
              </button>
            {/snippet}
          </DropdownMenuTrigger>
        {/snippet}
      </TooltipTrigger>
      <TooltipContent side="right">{item.label}</TooltipContent>
    </Tooltip>
    <DropdownMenuContent side="right" align="start">
      <DropdownMenuLabel>{item.label}</DropdownMenuLabel>
      {#each entries as entry (entry.id)}
        {@const EntryIcon = entry.icon}
        {@const current = isCurrentRow(entry, currentPath)}
        <DropdownMenuItem
          class={current
            ? 'bg-selected text-selected-foreground focus:bg-selected-hovered! focus:text-selected-foreground!'
            : undefined}
        >
          {#snippet child({ props })}
            <a
              {...props}
              href={entry.href}
              aria-current={current ? 'page' : undefined}
            >
              {#if EntryIcon}
                <EntryIcon aria-hidden="true" />
              {/if}
              {entry.label}
            </a>
          {/snippet}
        </DropdownMenuItem>
      {/each}
    </DropdownMenuContent>
  </DropdownMenu>
{/snippet}

{#snippet row(item: RailNavItem)}
  {@const current = isCurrentRow(item, currentPath)}
  {@const hasChildren = !collapsed && (item.children?.length ?? 0) > 0}
  {@const open = hasChildren && openSections.has(item.id)}
  {@const sectionsId = `${item.id}-sections`}
  <Tooltip disabled={!collapsed || openMenus.size > 0}>
    <TooltipTrigger>
      {#snippet child({ props }: { props: Record<string, unknown> })}
        {#if hasChildren}
          <div class="flex items-center gap-0.5">
            <span class="min-w-0 flex-1">
              {@render navLink(item, current, props)}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              class="shrink-0"
              aria-expanded={open}
              aria-controls={sectionsId}
              label={t(open ? 'shell.sections.hide' : 'shell.sections.show', {
                label: item.label,
              })}
              onclick={() => {
                toggleSection(item.id);
              }}
            >
              <ChevronRightIcon
                aria-hidden="true"
                class={['size-4 transition-transform', open && 'rotate-90']}
              />
            </Button>
          </div>
        {:else}
          {@render navLink(item, current, props)}
        {/if}
      {/snippet}
    </TooltipTrigger>
    <TooltipContent side="right">{item.label}</TooltipContent>
  </Tooltip>
  {#if open}
    <div
      id={sectionsId}
      class="ms-5 flex flex-col gap-0.5 border-s border-border"
    >
      {#each item.children ?? [] as child (child.id)}
        {@render row(child)}
      {/each}
    </div>
  {/if}
{/snippet}

{#snippet topRow(item: RailNavItem)}
  {#if collapsed && (item.children?.length ?? 0) > 0}
    {@render sectionMenu(item)}
  {:else}
    {@render row(item)}
  {/if}
{/snippet}

<TooltipProvider delayDuration={0}>
  <!-- The rail shares the top bar's surface. -->
  <div
    data-slot="rail"
    class={[
      'flex h-full flex-col gap-1 border-e border-border bg-muted p-2',
      collapsed ? 'w-15' : 'w-54',
    ]}
  >
    {#each ungrouped as item (item.id)}
      {@render topRow(item)}
    {/each}

    {#each grouped as run (run.group.id)}
      <p
        class={[
          'mt-3 px-2 text-xs font-medium text-muted-foreground uppercase',
          collapsed && 'sr-only',
        ]}
      >
        {run.group.label}
      </p>
      {#each run.items as item (item.id)}
        {@render topRow(item)}
      {/each}
    {/each}

    {#if settings}
      <Separator decorative={false} class="my-1.5" />
      {@render topRow(settings)}
    {/if}

    <div
      class={[
        'mt-auto flex shrink-0 gap-1 border-t border-border px-3 py-3',
        collapsed ? 'flex-col items-center' : 'flex-row items-center',
      ]}
    >
      {@render footer?.()}
      {#if onToggle !== undefined}
        <Button
          variant="ghost"
          size="icon-md"
          class={!collapsed ? 'ms-auto' : undefined}
          label={collapsed ? t('shell.expand') : t('shell.collapse')}
          aria-expanded={!collapsed}
          onclick={onToggle}
        >
          <PanelLeftIcon aria-hidden="true" />
        </Button>
      {/if}
    </div>
  </div>
</TooltipProvider>
