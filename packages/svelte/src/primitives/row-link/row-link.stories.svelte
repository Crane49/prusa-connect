<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Button } from '../button';
  import { Table, TableBody, TableCell, TableRow } from '../table';
  import { ABOVE_ROW_LINK, LINKED_ROW, ROW_LINK } from './row-link';

  const { Story } = defineMeta({
    title: 'Primitives/Row Link',
  });

  const ROWS = ['Benchy.bgcode', 'Calibration cube.bgcode', 'Housing.bgcode'];
</script>

<!--
  Three class strings, not a component. The name link stretches over its row,
  and the action button is lifted above that hit area so it stays pressable.
-->

<Story name="Whole Row Opens" asChild>
  <Table>
    <TableBody>
      {#each ROWS as name (name)}
        <TableRow class={LINKED_ROW}>
          <TableCell>
            <a href="#{name}" class={ROW_LINK}>{name}</a>
          </TableCell>
          <TableCell class="text-end">
            <Button variant="secondary" size="sm" class={ABOVE_ROW_LINK}>
              Print
            </Button>
          </TableCell>
        </TableRow>
      {/each}
    </TableBody>
  </Table>
</Story>
