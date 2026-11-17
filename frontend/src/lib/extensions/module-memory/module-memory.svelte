<!--
  One service module's memory in a listing row: what its process uses against
  its budget, a bar that follows the server's near and over readings, and how
  often it failed lately. A use the host could not read is said to be unknown,
  never drawn as 0 B. A module that is not running has no use to read, so it
  names its budget and draws no bar.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Progress } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import { failuresText, usageText, usageTone } from '../memory';
  import { overBudgetWarning, type ResourceMemory } from '../stops';

  let {
    memory,
    running,
  }: {
    memory: ResourceMemory | undefined;
    /** Whether the module's process runs, so there is a use to read. */
    running: boolean;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
  const KEY = 'app.settings.extensions.memory';

  const budget = $derived(memory?.budget_bytes ?? 0);
  const used = $derived(memory?.used_bytes);
  const text = $derived(
    memory === undefined ? '' : usageText(memory, t, running),
  );
  const failures = $derived(
    memory === undefined ? undefined : failuresText(memory, t),
  );
  const tone = $derived(
    usageTone(memory?.over_budget ?? false, memory?.near_budget ?? false),
  );
  const warning = $derived(running ? overBudgetWarning(memory, t) : undefined);
</script>

{#if memory !== undefined}
  <span
    class="flex min-w-24 flex-col gap-1 whitespace-normal"
    data-part="module-memory"
  >
    <span class="text-xs tabular-nums" data-part="memory-text">{text}</span>
    {#if !running}
      <!-- Nothing runs, so nothing is measured. -->
    {:else if used === undefined}
      <!-- Progress draws an unread value as a full pulsing fill, which reads as
           100 per cent; an unknown use is an empty track and the text says so. -->
      <span
        class="block h-2 w-full rounded-sm bg-border"
        aria-hidden="true"
        data-part="memory-bar"
        data-state="indeterminate"
      ></span>
    {:else}
      <!-- A reading past the budget fills the bar and no further. -->
      <Progress
        value={Math.min(used, budget)}
        max={budget}
        {tone}
        data-tone={tone}
        aria-label={t(`${KEY}.meter`)}
        aria-valuetext={text}
        data-part="memory-bar"
      />
    {/if}
    {#if warning !== undefined}
      <span class="text-xs text-warning-text" data-part="memory-warning">
        {warning}
      </span>
    {/if}
    {#if failures !== undefined}
      <span class="text-xs text-muted-foreground" data-part="memory-failures">
        {failures}
      </span>
    {/if}
  </span>
{/if}
