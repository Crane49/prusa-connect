<!--
  One dashboard, looked at or being changed.

  Looked at, it is its head and its cards. Being changed, the head becomes the
  edit bar, the grid draws its cells, every card carries its controls, and
  what is drawn is the draft and not the record (dashboard-editor.svelte.ts).
  Nothing reaches the core until Save. Cancel, a link or the back button that
  leaves the page, and a fresh read after a refused save each drop the draft
  after asking, when there is something to drop.

  The dashboard the home page wrote from the fleet is not stored, and Edit
  stores nothing either. Its first Save makes it, so it gains an id, a
  revision and a link (ADR-20261008-sww rule 12), and leaving without saving leaves
  no dashboard behind. The page stays where it is after that save.

  A card added from the gallery is scrolled into sight, said in the grid's
  live region, and given the focus once its settings close, because the
  first free place is often below the fold.

  Undo and redo are on the keyboard too, as in any editor, except while a
  field has focus, where the keys belong to the field.
-->
<script lang="ts">
  import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import { onMount } from 'svelte';

  import { validDashboardName } from '../../domain';
  import { t } from '../../i18n';
  import { Button, ConfirmDialog, EmptyState, toast } from '../../primitives';
  import { dashboardContext } from './context';
  import DashboardHeader from './dashboard-header.svelte';
  import type { DashboardAct } from './editor/acts';
  import EditBar from './editor/edit-bar.svelte';
  import ShareDialog from './editor/share-dialog.svelte';
  import TileGallery from './editor/tile-gallery.svelte';
  import TileSettings from './editor/tile-settings.svelte';
  import FaultNote from './fault-note.svelte';
  import BoardGrid from './grid/board-grid.svelte';
  import type { ShownDashboard } from './shown';

  let {
    shown,
    onAct,
  }: {
    shown: ShownDashboard;
    /** Opens the dashboard dialog, which the screen holds. */
    onAct: (act: DashboardAct) => void;
  } = $props();

  const { dashboards, editor, navigation } = dashboardContext();

  const editing = $derived(editor.editing && editor.dashboard === shown.id);
  const cards = $derived(editing ? editor.cards : shown.cards);
  const columns = $derived(
    editing ? (editor.draft?.columns ?? shown.columns) : shown.columns,
  );
  const density = $derived(
    editing ? (editor.draft?.density ?? shown.density) : shown.density,
  );

  let gallery = $state(false);
  let settingsOf = $state<string | undefined>(undefined);
  let sharing = $state<number | undefined>(undefined);
  let deleting = $state(false);
  /** What happens once the person agrees to drop the draft, while the dialog asks. */
  let dropping = $state<(() => void) | undefined>(undefined);
  /** The card the gallery added last, which takes the focus when its settings close. */
  let added = $state<string | undefined>(undefined);

  const stay = (): void => undefined;

  /** Opens the editor. A dashboard written from the fleet is stored on its first save. */
  const edit = (then?: () => void): void => {
    if (dashboards.busy) return;
    editor.start(shown);
    then?.();
  };

  const save = async (): Promise<void> => {
    // A second press while the first save is on its way would send the same
    // revision and be refused as somebody else's change.
    if (dashboards.busy || !editor.dirty) return;
    const request = editor.request();
    if (request === undefined) return;
    const id = editor.dashboard;

    // The store holds a dashboard it just made as the one on screen, so this
    // screen draws it from here on.
    const kept =
      id === undefined
        ? (await dashboards.create(request)) !== undefined
        : await dashboards.save(id, request);
    if (kept) {
      editor.stop();
      settingsOf = undefined;
      toast.success(t('dashboard.edit.saved', { name: request.name }));
    }
  };

  const cancel = (): void => {
    if (editor.dirty) {
      dropping = stay;

      return;
    }
    editor.stop();
    settingsOf = undefined;
  };

  /** Reads the dashboard again after a refused save, asking before a draft is dropped. */
  const reload = (): void => {
    const read = (): void => {
      void dashboards.reload();
    };
    if (editing && editor.dirty) {
      dropping = read;

      return;
    }
    editor.stop();
    read();
  };

  /** Gives the focus to the card the gallery added, once its settings close. */
  const focusAdded = (id: string): void => {
    document
      .querySelector<HTMLElement>(
        `[data-tile="${CSS.escape(id)}"] [role='group']`,
      )
      ?.focus({ preventScroll: true });
  };

  const duplicate = async (): Promise<void> => {
    const copyName = t('dashboard.copyName', { name: shown.name });
    const made = await dashboards.create({
      ...shown,
      name: validDashboardName(copyName) ? copyName : shown.name,
    });
    if (made !== undefined) navigation.open(made.id);
  };

  const remove = async (): Promise<void> => {
    deleting = false;
    const { id, name } = shown;
    if (id === undefined) return;
    if (await dashboards.remove(id)) {
      toast.success(t('dashboard.deleted', { name }));
      navigation.home();
    }
  };

  const keyed = (event: KeyboardEvent): void => {
    if (!editing) return;
    const target = event.target;
    if (
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      (target instanceof HTMLElement && target.isContentEditable)
    ) {
      return;
    }

    if (!(event.metaKey || event.ctrlKey)) return;
    const key = event.key.toLowerCase();

    if (key === 'z' && !event.shiftKey) {
      event.preventDefault();
      editor.undo();
    } else if ((key === 'z' && event.shiftKey) || key === 'y') {
      event.preventDefault();
      editor.redo();
    } else if (key === 's') {
      event.preventDefault();
      void save();
    }
  };

  // A browser asks before it leaves a page with changes nobody saved.
  const leaving = (event: BeforeUnloadEvent): void => {
    if (editing && editor.dirty) event.preventDefault();
  };

  // A link or the back button inside the app asks the same question in the
  // page's own dialog.
  onMount(() =>
    navigation.checkLeave((go) => {
      if (!(editing && editor.dirty)) return false;
      dropping = go;

      return true;
    }),
  );
</script>

<svelte:window onkeydown={keyed} onbeforeunload={leaving} />

<!-- One column that may shrink below its content. A grid's own column grows
     to the widest thing in it, and a long name would widen the page. -->
<div
  class="grid grid-cols-1 gap-4"
  data-part="dashboard"
  data-dashboard={shown.id ?? 'fleet'}
>
  {#if editing}
    <EditBar
      onAdd={() => {
        gallery = true;
      }}
      onRename={() => {
        const draft = editor.draft;
        if (draft !== undefined) {
          onAct({ kind: 'rename', name: draft.name });
        }
      }}
      onCancel={cancel}
      onSave={() => void save()}
    />
  {:else}
    <DashboardHeader
      id={shown.id}
      name={shown.name}
      visibility={shown.visibility}
      onEdit={() => {
        edit();
      }}
      onNew={() => {
        onAct({ kind: 'create' });
      }}
      onShare={() => {
        sharing = shown.id;
      }}
      onDuplicate={() => void duplicate()}
      onDelete={() => {
        deleting = true;
      }}
    />
  {/if}

  <FaultNote
    fault={dashboards.writeFault}
    onDismiss={() => {
      dashboards.dismiss();
    }}
    onReload={reload}
  />

  {#if cards.length === 0 && !editing}
    <EmptyState
      icon={LayoutDashboardIcon}
      headingLevel="h2"
      heading={t('dashboard.empty.heading')}
      description={dashboards.rights.write
        ? t('dashboard.empty.note')
        : t('dashboard.empty.readerNote')}
    >
      {#snippet action()}
        {#if dashboards.rights.write}
          <Button
            onclick={() => {
              edit(() => {
                gallery = true;
              });
            }}
          >
            <PencilIcon aria-hidden="true" />
            {t('dashboard.empty.start')}
          </Button>
        {/if}
      {/snippet}
    </EmptyState>
  {:else}
    {#if editing && cards.length === 0}
      <Button
        variant="secondary"
        size="lg"
        class="w-full"
        onclick={() => {
          gallery = true;
        }}
      >
        <PlusIcon aria-hidden="true" />
        {t('dashboard.empty.addFirst')}
      </Button>
    {/if}
    <BoardGrid
      tiles={cards}
      {columns}
      {density}
      {editing}
      revealed={added}
      onsettings={(id: string) => {
        editor.selected = id;
        settingsOf = id;
      }}
    />
  {/if}
</div>

<TileGallery
  open={gallery}
  onClose={() => {
    gallery = false;
  }}
  onAdded={(id: string) => {
    added = id;
    settingsOf = id;
  }}
/>
<TileSettings
  id={settingsOf}
  onClose={() => {
    settingsOf = undefined;
  }}
  onCloseAutoFocus={(event: Event) => {
    // The gallery button the panel would return to is gone with the gallery.
    if (added === undefined) return;
    event.preventDefault();
    focusAdded(added);
    added = undefined;
  }}
/>
<ShareDialog
  dashboard={sharing}
  onClose={() => {
    sharing = undefined;
  }}
/>
<ConfirmDialog
  open={deleting}
  onOpenChange={(next: boolean) => {
    deleting = next;
  }}
  title={t('dashboard.delete.title', { name: shown.name })}
  description={t('dashboard.delete.note')}
  confirmLabel={t('dashboard.action.delete')}
  onConfirm={() => void remove()}
/>
<ConfirmDialog
  open={dropping !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) dropping = undefined;
  }}
  title={t('dashboard.drop.title')}
  description={t('dashboard.drop.note')}
  confirmLabel={t('dashboard.drop.confirm')}
  onConfirm={() => {
    const then = dropping;
    dropping = undefined;
    editor.stop();
    settingsOf = undefined;
    then?.();
  }}
/>
