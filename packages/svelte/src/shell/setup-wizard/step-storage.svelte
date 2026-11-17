<!-- The folder the files a person uploads are kept in, checked on the server. -->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';

  import type { SetupStorageCheck } from '../../data';
  import { describeFault, t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    FaultAlert,
    Field,
    Input,
  } from '../../primitives';
  import HistoryBlock from './history-block.svelte';
  import type { SetupDraft } from './setup-draft.svelte';

  interface StepStorageProps {
    draft: SetupDraft;
    fallback: string;
    showErrors: boolean;
    /** The server's last answer about a folder. */
    check: SetupStorageCheck | undefined;
    /** Why the folder could not be checked at all. */
    error: Error | undefined;
    /** Whether the operator's file already declares a storage. */
    declared: boolean;
    docsHref?: string | undefined;
  }

  let {
    draft,
    fallback,
    showErrors,
    check,
    error,
    declared,
    docsHref,
  }: StepStorageProps = $props();

  const root = $derived(draft.storageRoot ?? fallback);
  const missing = $derived(
    showErrors && root.trim() === '' ? t('setupWizard.storage.missing') : '',
  );
  const fault = $derived(
    error === undefined ? undefined : describeFault(error),
  );

  const PROBLEM = $derived<Record<string, string>>({
    not_a_folder: t('setupWizard.storage.notAFolder'),
    not_writable: t('setupWizard.storage.notWritable'),
    no_parent: t('setupWizard.storage.noParent'),
  });

  // An answer about another folder than the one in the field says nothing.
  const current = $derived(
    check !== undefined &&
      (check.root === root.trim() || check.root.endsWith(root.trim()))
      ? check
      : undefined,
  );
</script>

<div class="flex flex-col gap-4" data-part="storage">
  {#if declared}
    <p class="text-sm text-muted-foreground">
      {t('setupWizard.storage.declared')}
    </p>
  {:else}
    <Field
      label={t('setupWizard.storage.label')}
      description={t('setupWizard.storage.nas')}
      error={missing}
      control={Input}
      controlProps={{
        value: root,
        type: 'text',
        class: 'font-mono',
        spellcheck: false,
        oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
          draft.storageRoot = event.currentTarget.value;
        },
      }}
    />
    {#if fault !== undefined}
      <FaultAlert {fault} />
    {:else if current?.writable === true}
      <p
        class="flex items-center gap-2 text-sm text-success-text"
        data-part="storage-ok"
      >
        {#if current.exists}
          <CircleCheckIcon class="size-4 shrink-0" aria-hidden="true" />
          {t('setupWizard.storage.ready')}
        {:else}
          <FolderPlusIcon class="size-4 shrink-0" aria-hidden="true" />
          {t('setupWizard.storage.willCreate')}
        {/if}
      </p>
    {:else if current !== undefined}
      <Alert variant="destructive" data-part="storage-problem">
        <AlertTitle>{PROBLEM[current.problem ?? 'not_writable']}</AlertTitle>
        <AlertDescription>
          <p>{t('setupWizard.storage.pickAnother')}</p>
          {#if current.detail !== undefined}
            <p class="mt-2 font-mono text-xs">{current.detail}</p>
          {/if}
        </AlertDescription>
      </Alert>
    {/if}
  {/if}
  <!-- Every type this build ships keeps files in a folder, so there is no
       choice of type to draw yet. The step says other kinds are planned. -->
  <p class="text-sm text-muted-foreground" data-part="storage-providers">
    {t('setupWizard.storage.providers')}
    {#if docsHref !== undefined}
      {@const docs = docsHref}
      <Button variant="link">
        {#snippet child({ props }: { props: Record<string, unknown> })}
          <a
            {...props}
            href={`${docs}storage/#writing-a-storage-provider`}
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('setupWizard.storage.providersLink')}
          </a>
        {/snippet}
      </Button>
    {/if}
  </p>
  <HistoryBlock {draft} />
</div>
