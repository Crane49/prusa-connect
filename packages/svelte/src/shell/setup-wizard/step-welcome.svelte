<!-- The first screen: what this server is, and the way to its documentation. -->
<script lang="ts">
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
  import BookOpenIcon from '@lucide/svelte/icons/book-open';
  import CheckIcon from '@lucide/svelte/icons/check';

  import { t } from '../../i18n';
  import { Button } from '../../primitives';

  interface StepWelcomeProps {
    docsHref: string | undefined;
    onstart: () => void;
  }

  let { docsHref, onstart }: StepWelcomeProps = $props();

  const points = $derived([
    t('setupWizard.welcome.pointLocal'),
    t('setupWizard.welcome.pointTime'),
    t('setupWizard.welcome.pointLater'),
  ]);
</script>

<div class="flex flex-col gap-8" data-part="welcome">
  <ul class="flex flex-col gap-3">
    {#each points as point (point)}
      <li class="flex items-center gap-3">
        <span
          class="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-link"
        >
          <CheckIcon class="size-4" aria-hidden="true" />
        </span>
        <span>{point}</span>
      </li>
    {/each}
  </ul>
  <div class="flex flex-wrap items-center gap-3">
    <Button size="lg" onclick={onstart}>
      {t('setupWizard.welcome.start')}
      <ArrowRightIcon aria-hidden="true" />
    </Button>
    {#if docsHref !== undefined}
      <Button variant="ghost">
        {#snippet child({ props }: { props: Record<string, unknown> })}
          <a
            {...props}
            href={docsHref}
            target="_blank"
            rel="noopener noreferrer"
          >
            <BookOpenIcon class="size-4" aria-hidden="true" />
            {t('setupWizard.welcome.docs')}
          </a>
        {/snippet}
      </Button>
    {/if}
  </div>
</div>
