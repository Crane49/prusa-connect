<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import DropdownMenuCheckboxItem from './dropdown-menu-checkbox-item.svelte';
  import DropdownMenuContent from './dropdown-menu-content.svelte';
  import DropdownMenuItem from './dropdown-menu-item.svelte';
  import DropdownMenuLabel from './dropdown-menu-label.svelte';
  import DropdownMenuGroup from './dropdown-menu-group.svelte';
  import DropdownMenuPortal from './dropdown-menu-portal.svelte';
  import DropdownMenuRadioGroup from './dropdown-menu-radio-group.svelte';
  import DropdownMenuRadioItem from './dropdown-menu-radio-item.svelte';
  import DropdownMenuSub from './dropdown-menu-sub.svelte';
  import DropdownMenuSubContent from './dropdown-menu-sub-content.svelte';
  import DropdownMenuSubTrigger from './dropdown-menu-sub-trigger.svelte';
  import DropdownMenuSeparator from './dropdown-menu-separator.svelte';
  import DropdownMenuShortcut from './dropdown-menu-shortcut.svelte';
  import DropdownMenuTrigger from './dropdown-menu-trigger.svelte';
  import DropdownMenu from './dropdown-menu.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/DropdownMenu',
    component: DropdownMenu,
  });
</script>

<script lang="ts">
  let autoConnect = $state(false);
  let sortBy = $state('name');
  let autoConnectOpen = $state(false);
</script>

{#snippet printerActions()}
  <DropdownMenu>
    <DropdownMenuTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Printer actions</Button>
      {/snippet}
    </DropdownMenuTrigger>
    <DropdownMenuContent>
      <DropdownMenuLabel>Actions</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem>
        Rename
        <DropdownMenuShortcut>⌘R</DropdownMenuShortcut>
      </DropdownMenuItem>
      <DropdownMenuItem>Duplicate</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuCheckboxItem bind:checked={autoConnect}>
        Auto-connect
      </DropdownMenuCheckboxItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem variant="destructive">Remove</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
{/snippet}

<Story name="Default" asChild children={printerActions} />

<Story name="Open State" asChild>
  <DropdownMenu open>
    <DropdownMenuTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Printer actions</Button>
      {/snippet}
    </DropdownMenuTrigger>
    <DropdownMenuContent>
      <DropdownMenuLabel>Actions</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem>
        Rename
        <DropdownMenuShortcut>⌘R</DropdownMenuShortcut>
      </DropdownMenuItem>
      <DropdownMenuItem>Duplicate</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuCheckboxItem bind:checked={autoConnectOpen}>
        Auto-connect
      </DropdownMenuCheckboxItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem variant="destructive">Remove</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</Story>

<Story name="Submenu and choices" asChild>
  <DropdownMenu open>
    <DropdownMenuTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Printer actions</Button>
      {/snippet}
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent>
        <DropdownMenuGroup>
          <DropdownMenuLabel>Sort by</DropdownMenuLabel>
          <DropdownMenuRadioGroup bind:value={sortBy}>
            <DropdownMenuRadioItem value="name">Name</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="state">State</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="progress"
              >Progress</DropdownMenuRadioItem
            >
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuSub open>
          <DropdownMenuSubTrigger>Move to</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuItem>Workshop</DropdownMenuItem>
            <DropdownMenuItem>Office</DropdownMenuItem>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenu>
</Story>

<Story
  name="Opens By Keyboard And Returns Focus On Escape"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerActions}
/>

<Story
  name="Arrow Key Navigation Moves The Active Item"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerActions}
/>

<Story
  name="Checkbox Item Aria Checked Follows Activation"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerActions}
/>
