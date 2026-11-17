<!--
  What people noted on the part, oldest first, and a box to add one.

  A comment is a person's own words, so only the person who wrote it is
  offered to change or delete it; the module refuses anybody else in any case.
  The box is drawn for a person who may write, and a comment that was refused
  keeps its text in the box.
-->
<script lang="ts">
  import MessageSquareIcon from '@lucide/svelte/icons/message-square';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';
  import { formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Button,
    ConfirmDialog,
    DropdownMenuItem,
    EmptyState,
    RowActions,
    Textarea,
    Label,
  } from '@prusa/connect-ui/primitives';

  import { MAX_COMMENT } from '../presentation';
  import type { Comment } from '../parts';
  import { heldSession } from '../session';
  import FaultNote from './fault-note.svelte';
  import { personOf } from './person';

  const { catalogue, host } = heldSession();
  const t = host.t;
  const uid = $props.id();

  let body = $state('');
  let editing = $state<{ id: number; body: string } | undefined>(undefined);
  let deleting = $state<Comment | undefined>(undefined);
  let refused = $state<unknown>(undefined);

  const part = $derived(catalogue.detail);
  const comments = $derived(part?.comments ?? []);

  /** Keeps what the module refused beside the box, and off the page header. */
  const keep = (): void => {
    refused = catalogue.fault;
    catalogue.dismiss();
  };

  const post = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    if (part === undefined || body.trim() === '' || catalogue.busy) return;
    refused = undefined;
    if (await catalogue.addComment(part.id, body.trim())) body = '';
    else keep();
  };

  const saveEdit = async (): Promise<void> => {
    if (
      part === undefined ||
      editing === undefined ||
      editing.body.trim() === ''
    )
      return;
    refused = undefined;
    if (
      await catalogue.changeComment(part.id, editing.id, editing.body.trim())
    ) {
      editing = undefined;
    } else {
      keep();
    }
  };

  const remove = (): void => {
    const comment = deleting;
    deleting = undefined;
    if (part === undefined || comment === undefined) return;
    void catalogue.deleteComment(part.id, comment.id);
  };
</script>

<div class="grid gap-y-4">
  <FaultNote fault={refused} />

  {#if comments.length === 0}
    <EmptyState
      icon={MessageSquareIcon}
      headingLevel="h2"
      heading={t('parts.comments.none.heading')}
      description={catalogue.allowed.write
        ? t('parts.comments.none.note')
        : t('parts.comments.none.viewer')}
    />
  {:else}
    <ol class="grid gap-y-3" data-part="comments" aria-live="polite">
      {#each comments as comment (comment.id)}
        <li class="grid gap-y-2 rounded-sm bg-surface p-3" data-part="comment">
          <div
            class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"
          >
            <span class="font-medium text-foreground">
              {personOf(comment.by, catalogue.who, t)}
            </span>
            <span>{formatDateTime(comment.created)}</span>
            {#if comment.edited}
              <span>{t('parts.comments.edited')}</span>
            {/if}
            {#if comment.by === catalogue.who && catalogue.allowed.write}
              <span class="ms-auto">
                <RowActions name={t('parts.comments.one')}>
                  <DropdownMenuItem
                    onSelect={() => {
                      editing = { id: comment.id, body: comment.body };
                    }}
                  >
                    <PencilLineIcon aria-hidden="true" />
                    {t('parts.comments.edit')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => {
                      deleting = comment;
                    }}
                  >
                    <Trash2Icon aria-hidden="true" />
                    {t('parts.comments.delete.action')}
                  </DropdownMenuItem>
                </RowActions>
              </span>
            {/if}
          </div>
          {#if editing?.id === comment.id}
            <Textarea
              aria-label={t('parts.comments.edit')}
              rows={3}
              data-byte-limit={MAX_COMMENT}
              bind:value={editing.body}
            />
            <div class="flex gap-2">
              <Button
                size="sm"
                disabled={catalogue.busy}
                onclick={() => void saveEdit()}
              >
                {t('parts.action.save')}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onclick={() => {
                  editing = undefined;
                }}
              >
                {t('parts.action.cancel')}
              </Button>
            </div>
          {:else}
            <p class="text-sm whitespace-pre-line">{comment.body}</p>
          {/if}
        </li>
      {/each}
    </ol>
  {/if}

  {#if catalogue.allowed.write}
    <form class="grid gap-y-2" onsubmit={(event) => void post(event)}>
      <Label for="{uid}-comment">{t('parts.comments.add')}</Label>
      <Textarea
        id="{uid}-comment"
        rows={3}
        data-byte-limit={MAX_COMMENT}
        placeholder={t('parts.comments.placeholder')}
        bind:value={body}
      />
      <div>
        <Button
          type="submit"
          disabled={body.trim() === '' || catalogue.busy}
          data-part="post-comment"
        >
          {t('parts.comments.post')}
        </Button>
      </div>
    </form>
  {/if}
</div>

<ConfirmDialog
  open={deleting !== undefined}
  onOpenChange={(open: boolean) => {
    if (!open) deleting = undefined;
  }}
  title={t('parts.comments.delete.title')}
  description={t('parts.comments.delete.note')}
  confirmLabel={t('parts.action.delete')}
  onConfirm={remove}
/>
