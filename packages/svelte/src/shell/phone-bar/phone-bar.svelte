<script lang="ts">
  import MenuIcon from '@lucide/svelte/icons/menu';
  import XIcon from '@lucide/svelte/icons/x';

  import { t } from '../../i18n';
  import {
    Button,
    Logo,
    Sheet,
    SheetClose,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetTrigger,
  } from '../../primitives';
  import { ConnectionStatus } from '../connection-status';
  import { productSubtitle } from '../product-subtitle';
  import { isCurrentRow, type RailNavItem, sectionEntries } from '../rail';
  import { ThemeToggle } from '../theme-toggle';
  import { UserMenu } from '../user-menu';
  import type { PhoneBarProps } from './types';

  let {
    productName,
    items,
    settings,
    currentPath,
    connection,
    principal,
    principalDetail,
    onSignOut,
    bell,
  }: PhoneBarProps = $props();

  let open = $state(false);

  const settingsRows = $derived(
    settings === undefined ? [] : sectionEntries(settings),
  );

  // `--app-phone-bar-height` is set at runtime by a ResizeObserver, so it
  // cannot be a design token; that forces these two arbitrary values.
  const sheetContentClass =
    // eslint-disable-next-line no-restricted-syntax -- see the comment above.
    'bottom-[calc(var(--app-phone-bar-height)-1px)] max-h-[calc(100svh-var(--app-phone-bar-height)-3rem)] overflow-hidden rounded-b-none bg-popover p-0';
  const sheetOverlayClass =
    // eslint-disable-next-line no-restricted-syntax -- see the comment above.
    'inset-x-0 top-0 bottom-[var(--app-phone-bar-height)]';
</script>

{#snippet row(item: RailNavItem)}
  {@const Icon = item.icon}
  {@const current = isCurrentRow(item, currentPath)}
  <li>
    <a
      href={item.href}
      aria-current={current ? 'page' : undefined}
      onclick={() => {
        open = false;
      }}
      class={[
        'flex min-h-touch-target items-center gap-2.5 rounded-sm border px-2.5 py-1.5 text-sm font-medium focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-inset',
        current
          ? 'border-selected bg-selected text-selected-foreground'
          : 'border-transparent text-muted-foreground hover:bg-accent hover:text-foreground',
      ]}
    >
      {#if Icon}
        <Icon aria-hidden="true" />
      {/if}
      {item.label}
    </a>
  </li>
{/snippet}

<nav
  aria-label={t('shell.nav')}
  data-slot="phone-bar"
  class="grid min-h-16 grid-cols-bar items-center gap-1 border-t border-border bg-background px-2 pt-2 pb-safe-bottom-2"
>
  <span
    class="relative flex w-fit min-w-0 items-center self-center px-2"
    title={connection === 'offline' ? t('shell.offlineDetail') : undefined}
  >
    <Logo size="sm" subtitle={productSubtitle(productName)} />
    {#if connection === 'offline'}
      <ConnectionStatus
        state="offline"
        presentation="dot"
        class="absolute top-0 right-1 ring-2 ring-background"
      />
    {/if}
  </span>

  <Sheet bind:open>
    {#if open}
      <!-- SheetClose, not the trigger again: bits-ui's trigger only opens. -->
      <SheetClose>
        {#snippet child({ props })}
          <Button
            {...props}
            variant="ghost"
            size="icon-lg"
            aria-expanded={open}
            label={t('shell.closeMenu')}
          >
            <XIcon aria-hidden="true" class="size-6" />
          </Button>
        {/snippet}
      </SheetClose>
    {:else}
      <SheetTrigger>
        {#snippet child({ props })}
          <Button
            {...props}
            variant="ghost"
            size="icon-lg"
            aria-expanded={open}
            label={t('shell.menu')}
          >
            <MenuIcon aria-hidden="true" class="size-6" />
          </Button>
        {/snippet}
      </SheetTrigger>
    {/if}

    <!-- preventScroll/trapFocus false: bits-ui has no non-modal option, so
         this skips its scroll lock and focus trap to keep the bar reachable. -->
    <SheetContent
      side="bottom"
      showCloseButton={false}
      preventScroll={false}
      trapFocus={false}
      onOpenAutoFocus={(event: Event) => {
        // Default focus moves to the connection glyph, opening its tooltip over the list.
        event.preventDefault();
      }}
      class={sheetContentClass}
      overlayClass={sheetOverlayClass}
    >
      <!-- One flex child: SheetContent's own gap-4 always wins the Tailwind
           cascade over a caller class, so a lone child avoids a gap. -->
      <div class="flex min-h-0 flex-1 flex-col">
        <SheetHeader
          class="flex-row items-center justify-between border-b border-border py-2"
        >
          <!-- A nested span, not a class on SheetTitle: Tailwind always
               orders text-base before its own text-lg, so a class here would lose. -->
          <SheetTitle
            ><span class="text-base">{t('shell.menu')}</span></SheetTitle
          >
          <span class="flex items-center gap-1">
            <ConnectionStatus state={connection} />
            <ThemeToggle />
            <UserMenu {principal} {principalDetail} {onSignOut} />
          </span>
        </SheetHeader>
        <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <ul class="flex flex-col gap-1 p-4">
            {#each items as item (item.id)}
              {#if (item.children?.length ?? 0) > 0}
                <!-- The rail draws a group's entries one step in under its
                     row, and the sheet keeps that shape so a module's pages
                     do not read as the core's own. -->
                <li>
                  <p
                    class="px-2.5 pt-2 pb-1 text-xs font-medium text-muted-foreground"
                  >
                    {item.label}
                  </p>
                  <ul
                    class="ms-5 flex flex-col gap-1 border-s border-border ps-1"
                  >
                    {#each sectionEntries(item) as entry (entry.id)}
                      {@render row(entry)}
                    {/each}
                  </ul>
                </li>
              {:else}
                {@render row(item)}
              {/if}
            {/each}
            {#if settings !== undefined}
              <li
                class="mt-3 border-t border-border px-2.5 pt-4 pb-1 text-xs font-medium text-muted-foreground"
              >
                {settings.label}
              </li>
              {#each settingsRows as item (item.id)}
                {@render row(item)}
              {/each}
            {/if}
          </ul>
        </div>
      </div>
    </SheetContent>
  </Sheet>

  <span class="flex items-center justify-end gap-1">{@render bell?.()}</span>
</nav>
