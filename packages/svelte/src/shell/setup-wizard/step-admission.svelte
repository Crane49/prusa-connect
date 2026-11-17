<!-- How a printer that connects for the first time joins. -->
<script lang="ts">
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
  import ZapIcon from '@lucide/svelte/icons/zap';

  import { t } from '../../i18n';
  import ChoiceCards from './choice-cards.svelte';
  import type { SetupAdmission, SetupDraft } from './setup-draft.svelte';

  let { draft }: { draft: SetupDraft } = $props();

  const options = $derived([
    {
      value: 'ask' as const,
      title: t('setupWizard.admission.ask.title'),
      note: t('setupWizard.admission.ask.note'),
      icon: ShieldCheckIcon,
      recommended: true,
    },
    {
      value: 'open' as const,
      title: t('setupWizard.admission.open.title'),
      note: t('setupWizard.admission.open.note'),
      icon: ZapIcon,
    },
  ]);
</script>

<ChoiceCards
  label={t('setupWizard.admission.title')}
  value={draft.admission}
  {options}
  onchange={(admission: SetupAdmission) => {
    draft.admission = admission;
  }}
/>
