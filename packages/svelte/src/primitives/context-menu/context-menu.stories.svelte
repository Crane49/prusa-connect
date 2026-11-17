<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ContextMenuCheckboxItem from './context-menu-checkbox-item.svelte';
  import ContextMenuContent from './context-menu-content.svelte';
  import ContextMenuItem from './context-menu-item.svelte';
  import ContextMenuLabel from './context-menu-label.svelte';
  import ContextMenuGroup from './context-menu-group.svelte';
  import ContextMenuPortal from './context-menu-portal.svelte';
  import ContextMenuRadioGroup from './context-menu-radio-group.svelte';
  import ContextMenuRadioItem from './context-menu-radio-item.svelte';
  import ContextMenuSub from './context-menu-sub.svelte';
  import ContextMenuSubContent from './context-menu-sub-content.svelte';
  import ContextMenuSubTrigger from './context-menu-sub-trigger.svelte';
  import ContextMenuSeparator from './context-menu-separator.svelte';
  import ContextMenuShortcut from './context-menu-shortcut.svelte';
  import ContextMenuTrigger from './context-menu-trigger.svelte';
  import ContextMenu from './context-menu.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ContextMenu',
    component: ContextMenu,
  });
</script>

<script lang="ts">
  let autoConnect = $state(false);
  let sortBy = $state('name');
</script>

<!-- bits-ui gives the trigger `tabindex="-1"`, the same as Radix does, so the
     region gets a tab stop of its own from `tabindex={0}`. -->
{#snippet actions()}
  <ContextMenuLabel>Actions</ContextMenuLabel>
  <ContextMenuSeparator />
  <ContextMenuItem>
    Rename
    <ContextMenuShortcut>⌘R</ContextMenuShortcut>
  </ContextMenuItem>
  <ContextMenuItem>Duplicate</ContextMenuItem>
  <ContextMenuSeparator />
  <ContextMenuCheckboxItem bind:checked={autoConnect}>
    Auto-connect
  </ContextMenuCheckboxItem>
  <ContextMenuSeparator />
  <ContextMenuItem variant="destructive">Remove</ContextMenuItem>
{/snippet}

{#snippet printerActions()}
  <ContextMenu>
    <ContextMenuTrigger
      tabindex={0}
      class="flex h-24 w-64 items-center justify-center rounded-sm border border-dashed border-border text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      Right-click this printer
    </ContextMenuTrigger>
    <ContextMenuContent children={actions} />
  </ContextMenu>
{/snippet}

<Story name="Default" asChild children={printerActions} />

<!-- bits-ui anchors the surface to the point the right button was pressed at,
     and an open state set from the outside has no such point, so this story
     draws the menu at the top left corner of the page. -->
<Story name="Open State" asChild>
  <ContextMenu open>
    <ContextMenuTrigger
      tabindex={0}
      class="flex h-24 w-64 items-center justify-center rounded-sm border border-dashed border-border text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      Right-click this printer
    </ContextMenuTrigger>
    <ContextMenuContent children={actions} />
  </ContextMenu>
</Story>

<Story name="Submenu and choices" asChild>
  <ContextMenu open>
    <ContextMenuTrigger
      tabindex={0}
      class="flex h-24 w-64 items-center justify-center rounded-sm border border-dashed border-border text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      Right-click this printer
    </ContextMenuTrigger>
    <ContextMenuPortal>
      <ContextMenuContent>
        <ContextMenuGroup>
          <ContextMenuLabel>Sort by</ContextMenuLabel>
          <ContextMenuRadioGroup bind:value={sortBy}>
            <ContextMenuRadioItem value="name">Name</ContextMenuRadioItem>
            <ContextMenuRadioItem value="state">State</ContextMenuRadioItem>
            <ContextMenuRadioItem value="progress"
              >Progress</ContextMenuRadioItem
            >
          </ContextMenuRadioGroup>
        </ContextMenuGroup>
        <ContextMenuSeparator />
        <ContextMenuSub open>
          <ContextMenuSubTrigger>Move to</ContextMenuSubTrigger>
          <ContextMenuSubContent>
            <ContextMenuItem>Workshop</ContextMenuItem>
            <ContextMenuItem>Office</ContextMenuItem>
          </ContextMenuSubContent>
        </ContextMenuSub>
      </ContextMenuContent>
    </ContextMenuPortal>
  </ContextMenu>
</Story>

<Story
  name="Opens On Context Menu Event And Returns Focus On Escape"
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
