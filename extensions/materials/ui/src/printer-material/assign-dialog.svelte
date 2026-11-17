<!--
  Chooses what a printer tool holds. Step 1 is the add dialog's material list.
  A material with spools in stock opens them, and picking one puts that spool
  on the tool. A material with none is assigned as itself, which only labels
  the tool.

  Spools on another tool are listed but cannot be picked, with where they are,
  because a spool a person looks for and cannot find is worse.
-->
<script lang="ts">
  import {
    Alert,
    AlertTitle,
    Button,
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    ListingSkeleton,
    Tile,
  } from '@prusa/connect-ui/primitives';
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import ChooseMaterial from '../add-dialog/choose-material.svelte';
  import type {
    AssignRequest,
    Detail,
    MaterialsService,
    Spool,
    Tool,
  } from '../api';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { OWN_MODULE } from '../own-module';
  import { refusalText } from './reasons';

  let {
    service,
    deviceId,
    tool,
    label,
    onclose,
    onassigned,
  }: {
    service: MaterialsService;
    deviceId: string;
    /** The tool to assign to; the dialog is open while there is one. */
    tool: Tool | undefined;
    /** The tool's name, such as Tool 1. */
    label: string;
    onclose: () => void;
    onassigned: () => void;
  } = $props();

  const t = heldHost().t;

  /** The material whose spools are open. */
  let material = $state.raw<Detail | undefined>(undefined);
  let spools = $state.raw<readonly Spool[] | undefined>(undefined);
  let busy = $state(false);
  let fault = $state.raw<unknown>(undefined);

  const refusal = $derived(
    refusalText(t, fault, 'assign', tool?.reported?.abbreviation ?? ''),
  );

  const close = (): void => {
    material = undefined;
    spools = undefined;
    fault = undefined;
    onclose();
  };

  const assign = async (body: AssignRequest): Promise<void> => {
    if (tool === undefined || busy) return;
    busy = true;
    fault = undefined;
    try {
      await service.assign(deviceId, tool.id, body);
      onassigned();
      close();
    } catch (thrown) {
      fault = thrown;
    } finally {
      busy = false;
    }
  };

  const choose = (detail: Detail): void => {
    if (detail.spools === 0) {
      void assign({ material_uuid: detail.uuid });
      return;
    }
    material = detail;
    spools = undefined;
    fault = undefined;
    // ponytail: the first page only, 100 spools of one material. Read the
    // next pages when a shelf holds more.
    service.spools(detail.uuid).then(
      (page) => {
        // An archived spool cannot go on a tool.
        if (material === detail) {
          spools = page.spools.filter((s) => s.archived_at == null);
        }
      },
      (thrown: unknown) => {
        if (material === detail) fault = thrown;
      },
    );
  };

  /** Where a spool is when it is not free to pick, and undefined when it is. */
  const taken = (spool: Spool): string | undefined => {
    if (spool.tool == null) return undefined;
    if (spool.tool.device_id !== deviceId) {
      return t('materials.printer.otherPrinter');
    }
    return spool.tool.tool_id === tool?.id
      ? t('materials.printer.thisTool')
      : t('materials.printer.otherTool');
  };

  const grams = (g: number): string =>
    formatNumber(g, { maximumFractionDigits: 0 });
</script>

<Dialog
  open={tool !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) close();
  }}
>
  <!-- The panel is portalled, so it carries the module's name for its
       utilities. -->
  <DialogContent data-module={OWN_MODULE} size="xl">
    <DialogHeader>
      <DialogTitle
        >{t('materials.printer.assignTo', { tool: label })}</DialogTitle
      >
    </DialogHeader>

    <div class="body flex flex-col gap-4">
      {#if refusal}
        <Alert variant="destructive" data-part="refusal">
          <AlertTitle>{refusal}</AlertTitle>
        </Alert>
      {:else}
        <FaultNote {fault} />
      {/if}

      {#if material === undefined}
        <ChooseMaterial {service} onchoose={choose} oncancel={close} />
      {:else}
        <div data-part="spools" class="flex flex-col gap-3">
          <div>
            <Button
              variant="ghost"
              size="sm"
              onclick={() => (material = undefined)}
            >
              {t('materials.printer.back')}
            </Button>
          </div>
          <p class="text-base font-semibold">{material.name}</p>
          {#if spools === undefined}
            <ListingSkeleton
              label={t('materials.printer.readingSpools')}
              rows={3}
            />
          {:else}
            <ul class="flex flex-col gap-2">
              {#each spools as spool (spool.id)}
                {@const where = taken(spool)}
                <li>
                  <Tile
                    size="sm"
                    disabled={where !== undefined || busy}
                    onclick={() => void assign({ spool_id: spool.id })}
                  >
                    <span class="flex items-center justify-between gap-3">
                      <span class="flex min-w-0 flex-col">
                        <span class="font-medium">
                          {t('materials.printer.spool', { id: spool.id })}
                        </span>
                        {#if spool.location !== '' || spool.notes !== ''}
                          <span class="truncate text-xs text-muted-foreground">
                            {[spool.location, spool.notes]
                              .filter((s) => s !== '')
                              .join(' · ')}
                          </span>
                        {/if}
                      </span>
                      <span class="shrink-0 text-sm text-muted-foreground">
                        {where ??
                          t('materials.printer.grams', {
                            g: grams(spool.remaining_weight_g),
                          })}
                      </span>
                    </span>
                  </Tile>
                </li>
              {/each}
              <li>
                <Tile
                  size="sm"
                  disabled={busy}
                  onclick={() =>
                    void assign({ material_uuid: material?.uuid ?? '' })}
                >
                  <span class="flex flex-col">
                    <span class="font-medium">
                      {t('materials.printer.materialOnly')}
                    </span>
                    <span class="text-xs text-muted-foreground">
                      {t('materials.printer.materialOnlyNote')}
                    </span>
                  </span>
                </Tile>
              </li>
            </ul>
          {/if}
        </div>
      {/if}
    </div>
  </DialogContent>
</Dialog>

<style>
  /* A long list scrolls inside the screen, under the title. */
  .body {
    max-height: calc(100dvh - var(--spacing) * 32);
    overflow-y: auto;
  }
</style>
