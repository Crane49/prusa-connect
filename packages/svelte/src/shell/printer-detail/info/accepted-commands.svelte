<!-- The verbs this printer answers to, in words. -->
<script lang="ts">
  import { t } from '../../../i18n';
  import {
    Badge,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
  } from '../../../primitives';
  import {
    isOfferedHere,
    labelKeyOf,
    rankOf,
    spellOut,
  } from '../commands/vocabulary';

  let {
    verbs,
    onPickCommand,
  }: {
    verbs: readonly string[];
    onPickCommand?: ((verb: string) => void) | undefined;
  } = $props();

  /** How many chips stand open. Beyond this the card stops being readable. */
  const SHOWN = 8;

  // A verb the vocabulary never heard of reads as its own last segment.
  const nameOf = (verb: string): string => {
    const key = labelKeyOf(verb);

    return key === undefined ? spellOut(verb) : t(key);
  };

  const ordered = $derived(
    [...verbs].sort((one, other) => rankOf(one) - rankOf(other)),
  );
  const open = $derived(ordered.slice(0, SHOWN));
  const rest = $derived(ordered.slice(SHOWN));
</script>

{#snippet chips(list: readonly string[])}
  <ul class="flex flex-wrap gap-2">
    {#each list as verb (verb)}
      <li>
        <!-- The wire token is the value the verb is. -->
        {#if onPickCommand !== undefined && isOfferedHere(verb)}
          <Badge variant="outline" title={verb}>
            {#snippet child({ props })}
              <!-- The name alone would read as "Pause. -->
              <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- a Badge child is the documented way to make a pressable badge -->
              <button
                {...props}
                type="button"
                aria-label={t('printerDetailInfo.capabilities.pick.label', {
                  name: nameOf(verb),
                })}
                onclick={() => onPickCommand?.(verb)}>{nameOf(verb)}</button
              >
            {/snippet}
          </Badge>
        {:else}
          <Badge variant="outline" title={verb}>{nameOf(verb)}</Badge>
        {/if}
      </li>
    {/each}
  </ul>
{/snippet}

{@render chips(open)}

{#if rest.length > 0}
  <Collapsible>
    <CollapsibleTrigger variant="inline">
      {t('printerDetailInfo.capabilities.more', { count: rest.length })}
    </CollapsibleTrigger>
    <CollapsibleContent class="pt-3">
      {@render chips(rest)}
    </CollapsibleContent>
  </Collapsible>
{/if}
