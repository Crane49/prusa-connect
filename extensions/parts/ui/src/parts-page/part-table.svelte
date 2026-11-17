<!--
  One page of parts as a table, for comparing them down a column: the name
  with its picture, the tags, the newest version, what its files weigh, and
  when the part last changed.

  The name is a stretched link, so the whole row opens the part, the way the
  Storage listing's rows open a file. Columns that need room are dropped where
  the page is narrow, and the name is never one of them.
-->
<script lang="ts">
  import { formatBytes, formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Badge,
    LINKED_ROW,
    ROW_LINK,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    THUMBNAIL_ROW_MIN_HEIGHT,
  } from '@prusa/connect-ui/primitives';

  import { pictureUrl } from '../presentation';
  import { heldSession } from '../session';
  import FolderLink from './folder-link.svelte';
  import PartPicture from './part-picture.svelte';

  const { catalogue, navigation, host, files } = heldSession();
  const t = host.t;
</script>

<Table data-part="table">
  <TableCaption class="sr-only">{t('parts.listing.parts')}</TableCaption>
  <TableHeader>
    <TableRow>
      <TableHead>{t('parts.column.name')}</TableHead>
      <TableHead class="hidden @2xl/parts:table-cell"
        >{t('parts.column.tags')}</TableHead
      >
      <TableHead>{t('parts.column.version')}</TableHead>
      <TableHead class="hidden @xl/parts:table-cell"
        >{t('parts.column.size')}</TableHead
      >
      <TableHead class="hidden @3xl/parts:table-cell"
        >{t('parts.column.changed')}</TableHead
      >
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each catalogue.rows as part (part.id)}
      <TableRow class={[THUMBNAIL_ROW_MIN_HEIGHT, LINKED_ROW]} data-part="row">
        <TableCell class="max-w-80">
          <div class="flex items-center gap-3">
            <PartPicture
              url={pictureUrl(files, part.picture)}
              class="size-14 shrink-0"
            />
            <FolderLink
              spot={{
                folder: navigation.spot.folder,
                part: part.id,
                q: navigation.spot.q,
                tag: navigation.spot.tag,
              }}
              class={['grid min-w-0 text-start', ROW_LINK]}
            >
              <span class="truncate font-medium" data-part="part-name"
                >{part.name}</span
              >
              {#if part.summary}
                <span class="truncate text-xs text-muted-foreground"
                  >{part.summary}</span
                >
              {/if}
            </FolderLink>
          </div>
        </TableCell>
        <TableCell class="hidden @2xl/parts:table-cell">
          <span class="flex flex-wrap gap-1">
            {#each part.tags as tag (tag)}
              <Badge variant="outline">{tag}</Badge>
            {/each}
          </span>
        </TableCell>
        <TableCell class="whitespace-nowrap">
          {#if part.latest}
            <span class="font-mono text-xs">
              {t('parts.version.short', { number: part.latest.number })}
            </span>
          {:else}
            <span class="text-xs text-muted-foreground"
              >{t('parts.card.noFiles')}</span
            >
          {/if}
        </TableCell>
        <TableCell
          class="hidden font-mono text-xs whitespace-nowrap @xl/parts:table-cell"
        >
          {part.latest ? formatBytes(part.latest.bytes) : ''}
        </TableCell>
        <TableCell
          class="hidden text-xs whitespace-nowrap @3xl/parts:table-cell"
        >
          {formatDateTime(part.changed)}
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
