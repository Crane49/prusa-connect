<!--
  The head of a dashboard while it is being changed. It replaces the head the
  dashboard is looked at with, so nobody mistakes the two: the name with its
  pencil, a card to add, a step back and forward, the grid itself
  (grid-settings.svelte), and the two ways out, save or drop what was changed.

  The bar stays at the top of the page while the grid scrolls under it, so
  Save is in reach from the bottom of a long dashboard.
-->
<script lang="ts">
  import { Badge, Button, Separator, TitleButton } from '../../../primitives';
  import { t } from '../../../i18n';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import Redo2Icon from '@lucide/svelte/icons/redo-2';
  import Undo2Icon from '@lucide/svelte/icons/undo-2';

  import { dashboardContext } from '../context';
  import GridSettings from './grid-settings.svelte';

  let {
    onAdd,
    onRename,
    onCancel,
    onSave,
  }: {
    onAdd: () => void;
    onRename: () => void;
    onCancel: () => void;
    onSave: () => void;
  } = $props();

  const { dashboards, editor } = dashboardContext();

  const draft = $derived(editor.draft);
</script>

{#snippet step(
  label: string,
  Icon: typeof Undo2Icon,
  run: () => void,
  enabled: boolean,
)}
  <Button
    variant="ghost"
    size="icon-md"
    {label}
    disabled={!enabled}
    onclick={run}
  >
    <Icon aria-hidden="true" />
  </Button>
{/snippet}

{#if draft !== undefined}
  <div
    data-part="edit-bar"
    class="sticky inset-bs-0 z-20 -mx-1 flex flex-wrap items-center justify-between gap-3 rounded-sm border border-border bg-card/95 px-3 py-2 shadow-sm backdrop-blur"
  >
    <div class="flex min-w-0 items-center gap-2">
      <Badge variant="stateActive" class="shrink-0"
        >{t('dashboard.edit.badge')}</Badge
      >
      <TitleButton
        level="section"
        icon={PencilLineIcon}
        data-part="dashboard-rename"
        label={t('dashboard.edit.rename', { name: draft.name })}
        onclick={onRename}
      >
        {draft.name}
      </TitleButton>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <Button variant="secondary" data-part="card-add" onclick={onAdd}>
        <PlusIcon aria-hidden="true" />
        {t('dashboard.edit.add')}
      </Button>
      <Separator orientation="vertical" class="mx-1 h-6" />
      {@render step(
        t('dashboard.edit.undo'),
        Undo2Icon,
        () => {
          editor.undo();
        },
        editor.canUndo,
      )}
      {@render step(
        t('dashboard.edit.redo'),
        Redo2Icon,
        () => {
          editor.redo();
        },
        editor.canRedo,
      )}
      <GridSettings />
      <Separator orientation="vertical" class="mx-1 h-6" />
      <Button
        variant="ghost"
        data-part="edit-cancel"
        onclick={onCancel}
        disabled={dashboards.busy}
      >
        {t('dashboard.action.cancel')}
      </Button>
      <Button
        data-part="edit-save"
        onclick={onSave}
        disabled={dashboards.busy || !editor.dirty}
      >
        {t('dashboard.action.save')}
      </Button>
    </div>
  </div>
{/if}
