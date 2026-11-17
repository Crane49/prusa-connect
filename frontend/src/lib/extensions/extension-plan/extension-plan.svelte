<!--
  The extensions an install also adds, because the release requires them and
  this server does not have them (ADR-20261027-e6k rule 14). Each line names
  the release chosen, who requires it, and every request it makes: the one
  approval covers all of them. A plan the server refused says who needs what. An enabled extension that
  reads or draws files of a type the install claims too is named before the
  install, which the server refuses for it (ADR-20261027-wvj).
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    FaultAlert,
    Skeleton,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type { InstallPlan } from '../../stores/module-catalogue.svelte';
  import { shortNameOf } from '../dependencies';
  import { describeExtensionFault } from '../fault';
  import {
    memoryEstimate,
    type ModuleMemoryPool,
    offerBudget,
    plannedBytes,
  } from '../memory';
  import { MemoryEstimate } from '../memory-estimate';
  import { RequestList } from '../request-list';
  import { listRequests, requestsOfOffer } from '../requests';

  let {
    plan,
    pool,
  }: {
    plan: InstallPlan | undefined;
    /** The memory pool, absent when the server did not say. */
    pool?: ModuleMemoryPool | undefined;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  // The modules the plan adds are enabled together, so each is judged against
  // the pool with the ones listed before it already in it.
  const poolBefore = (index: number): ModuleMemoryPool | undefined => {
    if (pool === undefined || plan?.state !== 'ready') return pool;
    const before = plannedBytes(plan.dependencies.slice(0, index));

    return { ...pool, committed_bytes: pool.committed_bytes + before };
  };

  const requiredBy = (lines: readonly string[]): string =>
    lines
      .map((line) => {
        const [module = '', range = ''] = line.split(' ');

        return `${shortNameOf(module)} ${range}`.trim();
      })
      .join(', ');
</script>

{#if plan?.state === 'loading'}
  <Skeleton
    class="h-4 w-64 max-w-full"
    role="status"
    aria-label={t('app.settings.extensions.plan.loading')}
    data-part="plan-loading"
  />
{:else if plan?.state === 'refused'}
  <div data-part="plan-refused">
    <FaultAlert fault={describeExtensionFault(plan.error, t)} />
  </div>
{/if}

{#if plan?.state === 'ready' && plan.conflicts.length > 0}
  <Alert variant="destructive" data-part="plan-conflicts">
    <AlertDescription>
      <p class="font-medium">{t('app.settings.extensions.plan.conflicts')}</p>
      <ul class="flex flex-col gap-1">
        {#each plan.conflicts as conflict (conflict.module + conflict.with + conflict.use)}
          <li data-part="plan-conflict">
            {t(
              conflict.use === 'file_view'
                ? 'app.settings.extensions.plan.conflict.view'
                : 'app.settings.extensions.plan.conflict.read',
              {
                module: shortNameOf(conflict.module),
                other: shortNameOf(conflict.with),
                types: conflict.types.join(', '),
              },
            )}
          </li>
        {/each}
      </ul>
    </AlertDescription>
  </Alert>
{/if}

{#if plan?.state === 'ready' && plan.dependencies.length > 0}
  <div class="flex flex-col gap-2 text-sm" data-part="plan">
    <p class="font-medium">
      {t('app.settings.extensions.plan.adds', {
        count: plan.dependencies.length,
      })}
    </p>
    <ul class="flex flex-col gap-3">
      {#each plan.dependencies as added, index (added.module)}
        {@const requests = requestsOfOffer(added)}
        <li class="flex flex-col gap-1" data-part="plan-module">
          <span class="flex flex-wrap items-center gap-2">
            <span class="font-medium">{added.title ?? added.name}</span>
            <code class="text-xs text-muted-foreground">{added.version}</code>
          </span>
          <span class="text-xs text-muted-foreground">
            {t('app.settings.extensions.plan.requiredBy', {
              names: requiredBy(added.required_by ?? []),
            })}
          </span>
          {#if added.process}
            <MemoryEstimate
              estimate={memoryEstimate({
                ...offerBudget(added.memory_mib),
                pool: poolBefore(index),
                heldBytes: 0,
              })}
            />
          {/if}
          {#if requests !== undefined}
            <RequestList
              part="plan-requests"
              instance={added.name}
              requests={listRequests(requests)}
              label={t('app.settings.extensions.plan.asks')}
            />
          {/if}
        </li>
      {/each}
    </ul>
    <p class="text-xs text-muted-foreground">
      {t('app.settings.extensions.plan.approval')}
    </p>
  </div>
{/if}
