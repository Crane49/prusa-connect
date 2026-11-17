<!-- Tools: each head of the printer, the slots of the feeder on it, the filament loaded in each, and what a multi-material unit is doing, as the printer reported them. -->
<script lang="ts">
  import type { Device } from '../../../domain';
  import { formatUnit, t } from '../../../i18n';
  import {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    KeyValueList,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    type KeyValueRow,
  } from '../../../primitives';

  type Tree = NonNullable<Device['tools']>;
  type Tool = NonNullable<Tree['tools'][string]>;

  let {
    tools,
    buildVolume,
  }: { tools: Device['tools']; buildVolume?: Device['build_volume'] } =
    $props();

  /** One row of the table: a head, or a slot of the feeder on a head. */
  interface Row {
    id: string;
    slot: boolean;
    /** Null is a tool the printer has switched off. */
    tool: Tool | null;
  }

  // Ids are whole numbers joined by dots, and order part by part as numbers,
  // so 0.10 comes after 0.2 and a head comes before its slots.
  const compareIds = (left: string, right: string): number => {
    const a = left.split('.').map(Number);
    const b = right.split('.').map(Number);

    for (let i = 0; i < Math.min(a.length, b.length); i += 1) {
      const step = (a[i] ?? 0) - (b[i] ?? 0);
      if (step !== 0) return step;
    }

    return a.length - b.length;
  };

  // A slot is listed under its head while the head's feeder covers it. A slot
  // the printer still holds under a feeder it switched off is left out.
  const covered = (tree: Tree, id: string): boolean => {
    const at = id.lastIndexOf('.');
    if (at < 0) return true;

    const parent = tree.tools[id.slice(0, at)];
    const index = Number(id.slice(at + 1));

    return (
      parent !== null &&
      parent !== undefined &&
      index < (parent.feeder?.slot_count ?? 0) &&
      covered(tree, id.slice(0, at))
    );
  };

  const rows = $derived<Row[]>(
    tools === undefined
      ? []
      : Object.keys(tools.tools)
          .filter((id) => covered(tools, id))
          .sort(compareIds)
          .map((id) => ({
            id,
            slot: id.includes('.'),
            tool: tools.tools[id] ?? null,
          })),
  );

  const flag = (value: boolean | undefined): string => {
    if (value === undefined) return t('printerDetailInfo.tools.unknown');

    return value
      ? t('printerDetailInfo.tools.yes')
      : t('printerDetailInfo.tools.no');
  };

  // A null material package instance is a reading: the tool has nothing
  // loaded. An absent one is a printer that did not say. A head with a feeder
  // holds no material; its slots do.
  const materialText = (tool: Tool): string => {
    const slots = tool.feeder?.slot_count ?? 0;
    const model = tool.feeder?.model;
    if (slots > 0 && model !== undefined && model !== '')
      return t('printerDetailInfo.tools.feederModel', { model, n: slots });
    if (slots > 0) return t('printerDetailInfo.tools.feeder', { n: slots });

    const loaded = tool.material_package_instance;
    if (loaded === undefined) return t('printerDetailInfo.tools.unknown');

    return loaded === null
      ? t('printerDetailInfo.tools.noMaterial')
      : loaded.package.material.abbreviation;
  };

  // The commands of the multi-material unit, as the one character its
  // firmware names each by. Any other command is shown by its character.
  const MMU_COMMANDS: Record<string, string> = {
    L: 'printerDetailInfo.tools.mmuLoad',
    U: 'printerDetailInfo.tools.mmuUnload',
    T: 'printerDetailInfo.tools.mmuToolChange',
    K: 'printerDetailInfo.tools.mmuCut',
    E: 'printerDetailInfo.tools.mmuEject',
    H: 'printerDetailInfo.tools.mmuHome',
  };

  // What the unit is doing: the command it runs and the step it is at by its
  // progress code, or idle when it runs none and its last step is done.
  const unitText = (feeder: NonNullable<Tool['feeder']>): string => {
    const code = feeder.mmu_progress_code ?? 0;
    const command = feeder.mmu_command_in_progress;
    if (command === undefined || command === '')
      return code === 0
        ? t('printerDetailInfo.tools.mmuIdle')
        : t('printerDetailInfo.tools.mmuStep', { code });

    const key = MMU_COMMANDS[command];

    return t('printerDetailInfo.tools.mmuBusy', {
      command:
        key === undefined
          ? t('printerDetailInfo.tools.mmuCommand', { command })
          : t(key),
      code,
    });
  };

  // One row for each head whose feeder reports what its unit is doing. A
  // printer with one head says MMU; one with more names the head.
  const unitRows = $derived<KeyValueRow[]>(
    tools === undefined
      ? []
      : Object.keys(tools.tools)
          .filter((id) => !id.includes('.'))
          .sort(compareIds)
          .flatMap((id) => {
            const feeder = tools.tools[id]?.feeder;
            if (feeder?.mmu_progress_code === undefined) return [];

            return [
              {
                id: `mmu-${id}`,
                label:
                  rows.filter((row) => !row.slot).length > 1
                    ? t('printerDetailInfo.tools.mmuOnHead', { n: id })
                    : t('printerDetailInfo.tools.mmu'),
                value: unitText(feeder),
              },
            ];
          }),
  );

  const volumeText = $derived(
    buildVolume === undefined
      ? undefined
      : t('printerDetailInfo.tools.volumeSize', {
          x: formatUnit(buildVolume.x_mm, 'mm', { maximumFractionDigits: 0 }),
          y: formatUnit(buildVolume.y_mm, 'mm', { maximumFractionDigits: 0 }),
          z: formatUnit(buildVolume.z_mm, 'mm', { maximumFractionDigits: 0 }),
        }),
  );

  const summary = $derived<KeyValueRow[]>(
    tools === undefined
      ? []
      : [
          {
            id: 'count',
            label: t('printerDetailInfo.tools.count'),
            value:
              tools.tool_count === undefined
                ? undefined
                : String(tools.tool_count),
          },
          {
            id: 'volume',
            label: t('printerDetailInfo.tools.volume'),
            value: volumeText,
          },
          ...unitRows,
        ],
  );
</script>

<Card>
  <CardHeader>
    <CardTitle>
      <h2 class="contents">{t('printerDetailInfo.tools.heading')}</h2>
    </CardTitle>
  </CardHeader>
  <CardContent class="flex flex-col gap-4">
    {#if tools === undefined}
      <p class="text-sm text-muted-foreground">
        {t('printerDetailInfo.tools.none')}
      </p>
    {:else}
      <KeyValueList rows={summary} />
      {#if rows.length > 0}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('printerDetailInfo.tools.tool')}</TableHead>
              <TableHead>{t('printerDetailInfo.tools.nozzle')}</TableHead>
              <TableHead>{t('printerDetailInfo.tools.highFlow')}</TableHead>
              <TableHead>{t('printerDetailInfo.tools.hardened')}</TableHead>
              <TableHead>{t('printerDetailInfo.tools.material')}</TableHead>
              <TableHead>{t('printerDetailInfo.tools.temperature')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each rows as row (row.id)}
              <TableRow data-tool={row.id}>
                <TableHead
                  scope="row"
                  class={row.slot
                    ? 'ps-6 font-normal text-muted-foreground'
                    : 'text-foreground'}
                >
                  {row.slot
                    ? t('printerDetailInfo.tools.slotNumber', { id: row.id })
                    : t('printerDetailInfo.tools.toolNumber', { n: row.id })}
                </TableHead>
                {#if row.tool === null}
                  <TableCell colspan={5} class="text-muted-foreground">
                    {t('printerDetailInfo.tools.off')}
                  </TableCell>
                {:else if row.slot}
                  <TableCell colspan={3}></TableCell>
                  <TableCell>{materialText(row.tool)}</TableCell>
                  <TableCell></TableCell>
                {:else}
                  <TableCell>
                    {row.tool.features?.nozzle_diameter === undefined
                      ? t('printerDetailInfo.tools.unknown')
                      : formatUnit(row.tool.features.nozzle_diameter, 'mm', {
                          maximumFractionDigits: 2,
                        })}
                  </TableCell>
                  <TableCell
                    >{flag(row.tool.features?.nozzle_high_flow)}</TableCell
                  >
                  <TableCell
                    >{flag(row.tool.features?.nozzle_hardened)}</TableCell
                  >
                  <TableCell>{materialText(row.tool)}</TableCell>
                  <TableCell>
                    {row.tool.nozzle_temp === undefined
                      ? t('printerDetailInfo.tools.unknown')
                      : formatUnit(row.tool.nozzle_temp, '°C', {
                          maximumFractionDigits: 0,
                        })}
                  </TableCell>
                {/if}
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </CardContent>
</Card>
