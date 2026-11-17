<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import BreadcrumbEllipsis from './breadcrumb-ellipsis.svelte';
  import BreadcrumbItem from './breadcrumb-item.svelte';
  import BreadcrumbLink from './breadcrumb-link.svelte';
  import BreadcrumbList from './breadcrumb-list.svelte';
  import BreadcrumbPage from './breadcrumb-page.svelte';
  import BreadcrumbSeparator from './breadcrumb-separator.svelte';
  import Breadcrumb from './breadcrumb.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Breadcrumb',
    component: Breadcrumb,
  });
</script>

{#snippet folder()}
  <Breadcrumb>
    <BreadcrumbList>
      <BreadcrumbItem>
        <BreadcrumbLink href="#">Printers</BreadcrumbLink>
      </BreadcrumbItem>
      <BreadcrumbSeparator />
      <BreadcrumbItem>
        <BreadcrumbEllipsis />
      </BreadcrumbItem>
      <BreadcrumbSeparator />
      <BreadcrumbItem>
        <BreadcrumbLink href="#">MK4S</BreadcrumbLink>
      </BreadcrumbItem>
      <BreadcrumbSeparator />
      <BreadcrumbItem>
        <BreadcrumbPage>USB files</BreadcrumbPage>
      </BreadcrumbItem>
    </BreadcrumbList>
  </Breadcrumb>
{/snippet}

<Story name="Default" asChild children={folder} />

<Story
  name="Current Page Is Not A Link And Has Aria Current"
  tags={['!dev', '!autodocs']}
  asChild
  children={folder}
/>

<Story
  name="Nav Has An Accessible Name"
  tags={['!dev', '!autodocs']}
  asChild
  children={folder}
/>
