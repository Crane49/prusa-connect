<!-- Basic or advanced: how many questions follow. -->
<script lang="ts">
  import SlidersIcon from '@lucide/svelte/icons/sliders-horizontal';
  import SparklesIcon from '@lucide/svelte/icons/sparkles';

  import { t } from '../../i18n';
  import ChoiceCards from './choice-cards.svelte';
  import type { SetupPath } from './model';
  import type { SetupDraft } from './setup-draft.svelte';

  let { draft }: { draft: SetupDraft } = $props();

  const options = $derived([
    {
      value: 'basic' as const,
      title: t('setupWizard.path.basic.title'),
      note: t('setupWizard.path.basic.note'),
      icon: SparklesIcon,
      recommended: true,
    },
    {
      value: 'advanced' as const,
      title: t('setupWizard.path.advanced.title'),
      note: t('setupWizard.path.advanced.note'),
      icon: SlidersIcon,
    },
  ]);
</script>

<ChoiceCards
  label={t('setupWizard.path.title')}
  value={draft.path}
  {options}
  onchange={(path: SetupPath) => {
    draft.path = path;
  }}
/>
