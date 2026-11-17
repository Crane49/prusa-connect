<!-- Where the reader is inside a folder tree. -->
<script lang="ts">
  import {
    Breadcrumb,
    BreadcrumbItem,
    BreadcrumbLink,
    BreadcrumbList,
    BreadcrumbPage,
    BreadcrumbSeparator,
  } from '../breadcrumb';
  import type { FolderTrailProps } from './types';

  let { crumbs, label, onOpen }: FolderTrailProps = $props();
</script>

<Breadcrumb {label}>
  <BreadcrumbList>
    {#each crumbs as crumb, index (crumb.path)}
      <BreadcrumbItem>
        {#if index === crumbs.length - 1}
          <BreadcrumbPage>{crumb.label}</BreadcrumbPage>
        {:else}
          <BreadcrumbLink>
            {#snippet child({ props })}
              <button
                {...props}
                type="button"
                onclick={() => onOpen(crumb.path)}
              >
                {crumb.label}
              </button>
            {/snippet}
          </BreadcrumbLink>
        {/if}
      </BreadcrumbItem>
      {#if index < crumbs.length - 1}
        <BreadcrumbSeparator />
      {/if}
    {/each}
  </BreadcrumbList>
</Breadcrumb>
