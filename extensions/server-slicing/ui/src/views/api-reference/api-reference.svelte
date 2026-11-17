<!--
  The API tab: this module is an HTTP API around the PrusaSlicer command line,
  and this tab says how to reach it from another machine. Getting started comes
  first: the address the page is served from, the token and its scopes, and a
  curl walk from a project to its G-code. The routes follow in the core's own
  API reference page, framed here with this module's document
  (ADR-20261027-zyf), so they read the way the core's routes read at /docs.
-->
<script lang="ts">
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CodeBlock,
    CopyField,
  } from '@prusa/connect-ui/primitives';
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

  import { jobFailures, operations, type Scope } from '../../api-reference';
  import type { Translate } from '../../api';

  let {
    apiBase,
    coreBase,
    tokensHref,
    referenceHref,
    t,
  }: {
    /** The module's mount on this server, such as https://host/api/v1/modules/<namespace>. */
    apiBase: string;
    /** The core's API on this server, such as https://host/api/v1. */
    coreBase: string;
    /** Where a person mints an API credential in the app. */
    tokensHref: string;
    /** The core's reference page showing this module's document, /docs?module=<namespace>. */
    referenceHref: string;
    t: Translate;
  } = $props();

  const all = operations();

  const SCOPES: Scope[] = ['read', 'operate', 'administer'];

  const setup = $derived(
    [
      `BASE=${apiBase}`,
      `CORE=${coreBase}`,
      `TOKEN=pcl_…   # ${t('slicer.api.tokenComment')}`,
    ].join('\n'),
  );

  const steps = [
    {
      id: 'store',
      code: `curl -sS -X PUT "$CORE/storages/local/content?path=projects/plate.3mf" \\
  -H "Authorization: Bearer $TOKEN" \\
  -H 'content-type: application/octet-stream' \\
  --data-binary @plate.3mf`,
    },
    {
      id: 'submit',
      code: `JOB=$(curl -sS -X POST "$BASE/jobs" \\
  -H "Authorization: Bearer $TOKEN" \\
  -H 'content-type: application/json' \\
  -d '{"source":{"storage":"local","path":"projects/plate.3mf"},
       "selection":{"bed":1,"items":[0,2]},
       "output":{"storage":"local","path":"sliced/plate-bed2"}}' | jq -r .id)`,
    },
    {
      id: 'upload',
      code: `JOB=$(curl -sS -X POST "$BASE/jobs/upload?name=plate.3mf&output_storage=local&bed=1&items=0,2" \\
  -H "Authorization: Bearer $TOKEN" \\
  -H 'content-type: application/octet-stream' \\
  --data-binary @plate.3mf | jq -r .id)`,
    },
    {
      id: 'poll',
      code: `while :; do
  JSON=$(curl -sS "$BASE/jobs/$JOB" -H "Authorization: Bearer $TOKEN")
  STATE=$(echo "$JSON" | jq -r .state)
  case $STATE in done|failed|canceled) break ;; esac
  sleep 2
done
echo "$JSON" | jq '{state, error, result: .result.path}'`,
    },
    {
      id: 'download',
      code: `FILE=$(echo "$JSON" | jq -r .result.path)
curl -sS -o "$(basename "$FILE")" "$CORE/storages/local/content?path=$FILE" \\
  -H "Authorization: Bearer $TOKEN"`,
    },
  ] as const;

  const heightFor = (text: string): string => {
    const lines = text.split('\n').length;
    if (lines <= 3) return 'h-24';
    if (lines <= 6) return 'h-32';
    return 'h-48';
  };
</script>

<section
  class="@container/api grid min-w-0 grid-cols-1 gap-4"
  aria-labelledby="slicer-api"
  data-part="api-reference"
>
  <div class="grid grid-cols-1 gap-1">
    <h2 id="slicer-api" class="text-base font-semibold">
      {t('slicer.api.title')}
    </h2>
    <p class="text-sm text-muted-foreground">{t('slicer.api.intro')}</p>
  </div>

  <Card>
    <CardContent class="grid min-w-0 grid-cols-1 gap-4">
      <h3 class="text-sm font-medium">{t('slicer.api.connect')}</h3>
      <CopyField label={t('slicer.api.base')} value={apiBase} />
      <p class="text-sm">
        {t('slicer.api.auth')}
        <Button variant="link">
          {#snippet child({ props })}
            <a {...props} href={tokensHref}>{t('slicer.api.tokens')}</a>
          {/snippet}
        </Button>.
      </p>
      <dl class="grid grid-cols-1 gap-2 text-sm" data-part="api-scopes">
        {#each SCOPES as scope (scope)}
          <div class="grid grid-cols-1 gap-1 @xl/api:grid-cols-4 @xl/api:gap-4">
            <dt>
              <Badge variant="outline">{t(`slicer.api.scope.${scope}`)}</Badge>
            </dt>
            <dd class="flex min-w-0 flex-wrap gap-1 @xl/api:col-span-3">
              {#each all.filter((op) => op.scope === scope) as op (op.id)}
                <code
                  class="rounded-sm bg-muted px-1.5 py-0.5 font-mono text-xs"
                  >{op.method} {op.path}</code
                >
              {/each}
            </dd>
          </div>
        {/each}
      </dl>
      <CodeBlock
        label={t('slicer.api.setup')}
        value={setup}
        heightClass="h-24"
      />
    </CardContent>
  </Card>

  <Card>
    <CardContent class="grid min-w-0 grid-cols-1 gap-4" data-part="api-example">
      <h3 class="text-sm font-medium">{t('slicer.api.example')}</h3>
      <ol class="grid grid-cols-1 gap-4">
        {#each steps as step, index (step.id)}
          <li class="grid min-w-0 grid-cols-1 gap-2">
            <p class="text-sm">
              <span class="font-medium">{index + 1}.</span>
              {t(`slicer.api.step.${step.id}`)}
            </p>
            <CodeBlock value={step.code} heightClass={heightFor(step.code)} />
          </li>
        {/each}
      </ol>
    </CardContent>
  </Card>

  <Card>
    <CardContent
      class="grid min-w-0 grid-cols-1 gap-2"
      data-part="api-failures"
    >
      <h3 class="text-sm font-medium">{t('slicer.api.failures')}</h3>
      <p class="text-sm text-muted-foreground">
        {t('slicer.api.failuresNote')}
      </p>
      <ul class="grid grid-cols-1 gap-1 text-sm">
        {#each jobFailures() as code (code)}
          <li class="flex flex-wrap items-baseline gap-x-2">
            <code class="font-mono text-xs">{code}</code>
            <span class="text-muted-foreground">{t(`error.${code}`)}</span>
          </li>
        {/each}
      </ul>
    </CardContent>
  </Card>

  <section class="grid min-w-0 grid-cols-1 gap-2" data-part="api-routes">
    <div class="flex flex-wrap items-baseline justify-between gap-2 text-sm">
      <h3 class="font-medium">{t('slicer.api.routes')}</h3>
      <Button variant="link" size="inline-icon" class="w-fit">
        {#snippet child({ props })}
          <a
            {...props}
            href={referenceHref}
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('slicer.api.openReference')}
            <ExternalLinkIcon aria-hidden="true" class="size-3.5" />
          </a>
        {/snippet}
      </Button>
    </div>
    <iframe
      src={`${referenceHref}&embedded=1`}
      title={t('slicer.api.routes')}
      class="h-dvh w-full rounded-sm border border-border bg-background"
      data-part="api-viewer"
    ></iframe>
  </section>
</section>
