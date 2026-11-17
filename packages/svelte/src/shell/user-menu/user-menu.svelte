<script lang="ts">
  import { t } from '../../i18n';
  import {
    Button,
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
  } from '../../primitives';
  import { subjectInitials } from './initials';
  import type { UserMenuProps } from './types';

  let { principal, principalDetail, onSignOut }: UserMenuProps = $props();

  let initials = $derived(
    principal === undefined ? '' : subjectInitials(principal),
  );

  const AVATAR =
    'grid shrink-0 place-items-center rounded-full bg-secondary font-semibold text-secondary-foreground';
</script>

<DropdownMenu>
  <DropdownMenuTrigger>
    {#snippet child({ props })}
      <Button
        {...props}
        variant="ghost"
        size="icon-md"
        label={principal ?? t('shell.menu')}
      >
        <span aria-hidden="true" class={[AVATAR, 'size-7 text-xs']}>
          {initials}
        </span>
      </Button>
    {/snippet}
  </DropdownMenuTrigger>
  <DropdownMenuContent align="end">
    {#if principal !== undefined}
      <DropdownMenuLabel class="flex items-center gap-3 py-2 font-normal">
        <span aria-hidden="true" class={[AVATAR, 'size-9 text-sm']}>
          {initials}
        </span>
        <span class="flex min-w-0 flex-col">
          <span class="truncate">{principal}</span>
          {#if principalDetail !== undefined && principalDetail !== ''}
            <span class="truncate text-xs text-muted-foreground">
              {principalDetail}
            </span>
          {/if}
        </span>
      </DropdownMenuLabel>
      {#if onSignOut !== undefined}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onSignOut}>
          {t('shell.signOut')}
        </DropdownMenuItem>
      {/if}
    {/if}
  </DropdownMenuContent>
</DropdownMenu>
