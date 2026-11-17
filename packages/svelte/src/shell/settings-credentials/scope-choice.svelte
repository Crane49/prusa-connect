<!-- One scope out of the ones offered, each with what it lets a credential do. -->
<script lang="ts">
  import type { CredentialScope } from '../../data';
  import { t } from '../../i18n';
  import { Label, RadioGroup, RadioGroupItem } from '../../primitives';
  import { SCOPE_NAME, SCOPE_NOTE } from './types';

  let {
    offered,
    value,
    label,
    onChange,
  }: {
    /** The scopes to choose from, from the narrowest up. */
    offered: readonly CredentialScope[];
    value: CredentialScope;
    /** What the group is called for a screen reader. */
    label: string;
    onChange: (next: CredentialScope) => void;
  } = $props();

  const id = $props.id();
</script>

<RadioGroup
  class="gap-3"
  {value}
  onValueChange={(next: string) => {
    const chosen = offered.find((scope) => scope === next);
    if (chosen !== undefined) onChange(chosen);
  }}
  aria-label={label}
>
  {#each offered as scope (scope)}
    <div class="flex items-start gap-3">
      <RadioGroupItem value={scope} id={`${id}-${scope}`} class="mt-1" />
      <div class="flex min-w-0 flex-col gap-0.5">
        <Label variant="option" for={`${id}-${scope}`}>
          {t(SCOPE_NAME[scope])}
        </Label>
        <span class="text-caption text-muted-foreground">
          {t(SCOPE_NOTE[scope])}
        </span>
      </div>
    </div>
  {/each}
</RadioGroup>
