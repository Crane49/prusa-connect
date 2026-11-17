<!--
  The beds of a 3MF project as a strip of small cards, each with the picture
  the slicer saved of it and how many copies stand on it. Pressing one shows
  that bed on the plan and its objects in the list.

  The strip scrolls sideways rather than wrapping, so a project of twenty
  plates keeps the plan where it is. A card is a plain button with
  `aria-pressed`: no primitive draws a picture inside a toggle, and the
  toggle group's items are one line tall.
-->
<script lang="ts">
  import CircleDashedIcon from '@lucide/svelte/icons/circle-dashed';
  import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';

  import { Tile } from '@prusa/connect-ui/primitives';

  import { t } from '../i18n';
  import { ALL_BEDS, getProjectBrowser, OFF_BEDS } from './browser.svelte';
  import { bedLabel } from './labels';
  import { bedKey, copiesIn, type Project } from './model';

  interface Props {
    project: Project;
    /** Where the picture at an index of the project's thumbnails is served. */
    thumbnailUrl?: ((index: number) => string) | undefined;
  }

  let { project, thumbnailUrl }: Props = $props();

  const browser = getProjectBrowser();

  const cards = $derived(
    browser.views.map((view) => {
      if (view === ALL_BEDS) {
        return {
          view,
          label: t('projectView.plan.all'),
          picture: undefined,
          copies: project.counts.instances,
          off: false,
        };
      }
      if (view === OFF_BEDS) {
        return {
          view,
          label: t('projectView.plan.offBed'),
          picture: undefined,
          copies: copiesIn(project, 'off').length,
          off: true,
        };
      }
      const ref = browser.beds.find((bed) => bedKey(bed) === view) ?? {
        setup: 0,
        bed: 0,
      };

      return {
        view,
        label: bedLabel(project, ref),
        picture: project.setups[ref.setup]?.beds[ref.bed]?.thumbnail,
        copies: copiesIn(project, ref).length,
        off: false,
      };
    }),
  );
</script>

{#if cards.length > 1}
  <div
    class="-mx-1 overflow-x-auto px-1 pbe-1"
    data-slot="project-bed-switcher"
  >
    <ul class="flex w-max gap-2" aria-label={t('projectView.plan.views')}>
      {#each cards as card (card.view)}
        {@const pressed = browser.view === card.view}
        <li>
          <Tile
            size="sm"
            {pressed}
            off={card.off}
            class="max-w-60 min-w-36"
            onclick={() => {
              browser.view = card.view;
            }}
          >
            <span class="flex items-center gap-2">
              <span
                class="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-xs bg-muted"
              >
                {#if card.picture !== undefined && thumbnailUrl !== undefined}
                  <img
                    src={thumbnailUrl(card.picture)}
                    alt=""
                    loading="lazy"
                    class="size-full object-contain"
                  />
                {:else if card.off}
                  <CircleDashedIcon
                    aria-hidden="true"
                    class="size-5 text-destructive-text"
                  />
                {:else}
                  <LayoutGridIcon
                    aria-hidden="true"
                    class="size-5 text-muted-foreground"
                  />
                {/if}
              </span>
              <span class="grid min-w-0 grid-cols-1 content-start">
                <span class="truncate text-xs font-medium" title={card.label}>
                  {card.label}
                </span>
                <span class="text-xs text-muted-foreground tabular-nums">
                  {card.copies === 0
                    ? t('projectView.plan.emptyBed')
                    : t('projectView.bed.copies', { count: card.copies })}
                </span>
              </span>
            </span>
          </Tile>
        </li>
      {/each}
    </ul>
  </div>
{/if}
