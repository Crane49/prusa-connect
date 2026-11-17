<!--
  The fields of the version dialog: the files of the newest version to keep,
  the files to add, and the note.

  Dropped files go up at once, so the person writes the note while the bytes
  travel. Each row says where its file is: on its way, uploaded, or refused,
  and a refusal is drawn by its code, which is how an extension the storage
  does not take is reported. The version is saved once nothing is still on
  its way and at least one file is in it.

  The browser reports no progress while a request body goes up, so a row says
  "uploading" and the size rather than drawing a bar that would sit at zero.

  The draft is thrown away when the form goes, whatever takes it away: the
  dialog closing, or the whole part view leaving the screen because the person
  went back or followed a link. A draft that was saved keeps its files, and
  one still being saved waits for the answer. While it is being saved nothing
  in the form changes and it cannot be cancelled.
-->
<script lang="ts">
  import XIcon from '@lucide/svelte/icons/x';
  import { formatBytes } from '@prusa/connect-ui/i18n';
  import {
    Button,
    Checkbox,
    ClippedName,
    DialogFooter,
    FileDropzone,
    Label,
    Textarea,
  } from '@prusa/connect-ui/primitives';
  import { onDestroy, onMount, untrack } from 'svelte';

  import { faultText } from '../faults';
  import { glyphOf, MAX_NOTE } from '../presentation';
  import type { PartDetail } from '../parts';
  import { heldSession } from '../session';
  import { VersionDraft } from '../version-draft.svelte';
  import FaultNote from './fault-note.svelte';

  let {
    part,
    dropped = [],
    onSaving,
    onCancel,
    onSaved,
  }: {
    part: PartDetail;
    /** Files dropped onto the part's page, which go up as soon as the form opens. */
    dropped?: readonly File[];
    /** Told when saving starts and when it ends, so the dialog holds itself open meanwhile. */
    onSaving?: (saving: boolean) => void;
    onCancel: () => void;
    onSaved: () => void;
  } = $props();

  const { catalogue, files, host } = heldSession();
  const t = host.t;
  const uid = $props.id();

  // One draft per opening of the dialog: the form is made new each time.
  const draft = new VersionDraft(
    files,
    untrack(() => part),
  );
  let refused = $state<unknown>(undefined);

  onMount(() => {
    if (dropped.length > 0) draft.add(dropped);
  });

  onDestroy(() => {
    void draft.discard();
  });

  const UPLOAD_STATE: Record<string, string> = {
    sending: 'parts.upload.sending',
    done: 'parts.upload.done',
    failed: 'parts.upload.failed',
  };

  // An empty list is the server saying the storage takes any extension.
  const accept = $derived.by(() => {
    const allowed = catalogue.storage?.allowed_extensions ?? [];

    return allowed.length === 0 ? undefined : allowed.join(',');
  });

  const save = async (): Promise<void> => {
    if (!draft.ready || catalogue.busy) return;
    refused = undefined;
    onSaving?.(true);

    const saved = await draft.save(
      async (note, chosen) => await catalogue.addVersion(part.id, note, chosen),
    );
    onSaving?.(false);

    if (saved) {
      onSaved();
    } else {
      refused = catalogue.fault;
      catalogue.dismiss();
    }
  };
</script>

<div class="grid gap-y-4">
  <FaultNote fault={refused} />

  {#if draft.kept.length > 0}
    <fieldset class="grid gap-y-2">
      <legend class="mb-1 text-sm font-medium"
        >{t('parts.versionDialog.keep')}</legend
      >
      {#each draft.kept as row (`${row.file.storage}/${row.file.path}`)}
        {@const Glyph = glyphOf(row.file.name)}
        <div class="flex min-w-0 items-center gap-2 text-sm">
          <Checkbox
            id="{uid}-keep-{row.file.path}"
            checked={row.kept}
            disabled={draft.saving}
            onCheckedChange={(next: boolean) => {
              draft.keep(row.file, next);
            }}
          />
          <Glyph
            aria-hidden="true"
            class="size-4 shrink-0 text-muted-foreground"
          />
          <Label
            for="{uid}-keep-{row.file.path}"
            variant="option"
            class="min-w-0 flex-1"
          >
            <ClippedName name={row.file.name} />
          </Label>
          <span class="font-mono text-xs text-muted-foreground"
            >{formatBytes(row.file.size)}</span
          >
        </div>
      {/each}
    </fieldset>
  {/if}

  <div class="grid gap-y-2">
    <span class="text-sm font-medium">{t('parts.versionDialog.add')}</span>
    <FileDropzone
      {accept}
      disabled={draft.saving}
      onFiles={(dropped: File[]) => {
        draft.add(dropped);
      }}
    />
  </div>

  {#if draft.uploads.length > 0}
    <ul
      class="grid max-h-64 gap-y-2 overflow-y-auto"
      aria-live="polite"
      data-part="uploads"
    >
      {#each draft.uploads as upload (upload.key)}
        {@const Glyph = glyphOf(upload.name)}
        {@const refusal =
          upload.fault === undefined ? undefined : faultText(upload.fault, t)}
        <li
          class="grid min-w-0 gap-y-1 rounded-sm bg-surface p-2 text-sm"
          data-part="upload"
          data-state={upload.state}
        >
          <span class="flex min-w-0 items-center gap-2">
            <Glyph
              aria-hidden="true"
              class="size-4 shrink-0 text-muted-foreground"
            />
            <ClippedName name={upload.name} class="min-w-0 flex-1" />
            <span
              class={[
                'text-xs whitespace-nowrap',
                upload.state === 'failed'
                  ? 'text-destructive-text'
                  : 'text-muted-foreground',
              ]}
            >
              {t(UPLOAD_STATE[upload.state] ?? 'parts.upload.sending')} · {formatBytes(
                upload.size,
              )}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              label={t('parts.upload.remove', { name: upload.name })}
              disabled={draft.saving}
              onclick={() => void draft.drop(upload.key)}
            >
              <XIcon aria-hidden="true" />
            </Button>
          </span>
          {#if refusal}
            <span class="text-xs text-destructive-text">
              {refusal.title}{#if refusal.code}<code class="ms-1"
                  >{refusal.code}</code
                >{/if}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <div class="grid gap-y-2">
    <Label for="{uid}-note">{t('parts.versionDialog.noteLabel')}</Label>
    <Textarea
      id="{uid}-note"
      rows={3}
      data-byte-limit={MAX_NOTE}
      placeholder={t('parts.versionDialog.notePlaceholder')}
      bind:value={draft.note}
    />
  </div>

  <DialogFooter>
    <Button
      type="button"
      variant="secondary"
      disabled={draft.saving}
      data-part="cancel-version"
      onclick={onCancel}
    >
      {t('parts.action.cancel')}
    </Button>
    <Button
      disabled={!draft.ready || catalogue.busy}
      aria-busy={draft.saving}
      data-part="save-version"
      onclick={() => void save()}
    >
      {draft.sending
        ? t('parts.versionDialog.waiting')
        : t('parts.versionDialog.save')}
    </Button>
  </DialogFooter>
</div>
