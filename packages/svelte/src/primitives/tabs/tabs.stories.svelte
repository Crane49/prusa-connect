<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Tabs from './tabs.svelte';
  import TabsContent from './tabs-content.svelte';
  import TabsList from './tabs-list.svelte';
  import TabsTrigger from './tabs-trigger.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Tabs',
    component: Tabs,
  });
</script>

{#snippet printerTabs()}
  <Tabs value="printer" class="w-80">
    <TabsList>
      <TabsTrigger value="printer">Printer</TabsTrigger>
      <TabsTrigger value="camera">Camera</TabsTrigger>
      <TabsTrigger value="files">Files</TabsTrigger>
    </TabsList>
    <TabsContent value="printer">
      <p class="text-sm text-muted-foreground">MK4S — ready to print.</p>
    </TabsContent>
    <TabsContent value="camera">
      <p class="text-sm text-muted-foreground">No camera connected.</p>
    </TabsContent>
    <TabsContent value="files">
      <p class="text-sm text-muted-foreground">12 files on USB.</p>
    </TabsContent>
  </Tabs>
{/snippet}

<Story name="Default" asChild children={printerTabs} />

<Story
  name="Arrow Key Navigation Moves The Active Trigger"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerTabs}
/>

<Story
  name="Disabled Trigger Is Skipped By Arrow Keys"
  tags={['!dev', '!autodocs']}
  asChild
>
  <Tabs value="printer" class="w-80">
    <TabsList>
      <TabsTrigger value="printer">Printer</TabsTrigger>
      <TabsTrigger value="camera" disabled>Camera</TabsTrigger>
      <TabsTrigger value="files">Files</TabsTrigger>
    </TabsList>
    <TabsContent value="printer">Printer panel</TabsContent>
    <TabsContent value="camera">Camera panel</TabsContent>
    <TabsContent value="files">Files panel</TabsContent>
  </Tabs>
</Story>
