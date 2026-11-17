import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { dump, load } from 'js-yaml';

// A file URL pathname is /C:/... on Windows, which resolve() turns into C:\C:\...
const root = fileURLToPath(new URL('..', import.meta.url));
// Both documents are frozen captures of the retired server, which outlived the
// code they describe (ADR-0013 rule 8). The MQTT one is the adapter's own
// fixture and its topic table is gated against it.
const mqttSpecPath = resolve(root, '../adapters/events/mqtt/testdata/shipping-asyncapi.yaml');
const sseSpecPath = resolve(root, '../internal/arch/testdata/shipping-sse-asyncapi.yaml');
const mqttPagePath = resolve(root, 'src/content/docs/mqtt-asyncapi.mdx');
const ssePagePath = resolve(root, 'src/content/docs/sse-asyncapi.mdx');
const legacyAsyncApiDir = resolve(root, 'public/asyncapi');

const readYaml = async (path) => load(await readFile(path, 'utf8'));

const formatJson = (value) => JSON.stringify(value, null, 2);
const formatYaml = (value) => dump(value, { lineWidth: 1000 }).trim();

const mqttExampleOverrides = {
  serverOnline: '1',
  state: 'PRINTING',
  online: '1',
  material: 'PLA',
  speed: 100,
  tempNozzleCurrent: 215,
  tempNozzleTarget: 220,
  tempBedCurrent: 60,
  tempBedTarget: 60,
  tempChamberCurrent: 35,
  axisX: 123.45,
  axisY: 87.65,
  axisZ: 3.42,
  currentJob: 42,
  jobProgress: 17,
  jobState: 'PRINTING',
  jobProgressById: 17,
  jobTimeRemaining: 1240,
  slotActive: 1,
  slotTempCurrent: 215,
  slotMaterial: 'PLA',
  allPrinterData: '# subscribe to all data for a single printer',
  allPrinters: '# subscribe to all printers',
};

const sseExampleOverrides = {
  'prusa-connect.printer.connected': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.printer.connected',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      uuid: 'PRINTER_ID',
      model: 'MK4',
      firmware: '6.4.0',
      state: 'IDLE',
      connected: true,
      telemetry: {
        temp_nozzle: 25,
        temp_bed: 23,
        speed: 100,
      },
    },
  },
  'prusa-connect.printer.initial_state': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect',
    type: 'prusa-connect.printer.initial_state',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: [
      {
        uuid: 'PRINTER_ID',
        model: 'MK4',
        firmware: '6.4.0',
        state: 'IDLE',
        connected: true,
        telemetry: {
          temp_nozzle: 25,
          temp_bed: 23,
        },
      },
    ],
  },
  'prusa-connect.printer.state_changed': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.printer.state_changed',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      old_state: 'IDLE',
      new_state: 'PRINTING',
    },
  },
  'prusa-connect.transfer.update': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.transfer.update',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      file_id: 42,
      path: '/usb/upload.gcode',
      progress: 0.5,
      state: 'DOWNLOADING',
      started_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:05Z',
      total_size: 1024000,
      transferred: 512000,
    },
  },
  'prusa-connect.job.update': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.job.update',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      event: 'pause',
    },
  },
  'prusa-connect.file.changed': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.file.changed',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      old_path: '/usb/old.gcode',
      new_path: '/usb/new.gcode',
    },
  },
  'prusa-connect.printer.event': {
    specversion: '1.0',
    id: '00000000-0000-0000-0000-000000000000',
    source: '/prusa-connect/printer/PRINTER_ID',
    type: 'prusa-connect.printer.event',
    time: '2026-01-01T00:00:00Z',
    datacontenttype: 'application/json',
    data: {
      event: 'REJECTED',
      data: {
        command_id: 42,
        reason: 'Unknown command',
      },
    },
  },
};

function sampleScalar(schema, keyName = '') {
  if (schema?.enum?.length) return schema.enum[0];
  switch (schema?.type) {
    case 'integer':
      return keyName.includes('id') ? 42 : 1;
    case 'number':
      return 42.5;
    case 'boolean':
      return true;
    case 'null':
      return null;
    case 'string':
      if (keyName === 'specversion') return '1.0';
      if (keyName === 'source') return '/prusa-connect/printer/PRINTER_ID';
      if (keyName === 'datacontenttype') return 'application/json';
      if (keyName === 'time') return '2026-01-01T00:00:00Z';
      if (keyName === 'id') return '00000000-0000-0000-0000-000000000000';
      if (keyName === 'printerId') return 'PRINTER_ID';
      if (keyName === 'jobId') return '42';
      if (keyName === 'slotId') return '1';
      return 'example';
    default:
      return 'example';
  }
}

function resolveRef(schema, components) {
  if (!schema?.$ref) return schema;
  const ref = schema.$ref.replace('#/components/schemas/', '');
  return components?.schemas?.[ref] ?? schema;
}

function sampleValue(schema, components, keyName = '') {
  const resolved = resolveRef(schema, components);
  if (!resolved || typeof resolved !== 'object') return null;
  if (resolved.enum || typeof resolved.type === 'string') {
    if (resolved.type === 'object') {
      const result = {};
      const props = resolved.properties ?? {};
      const required = resolved.required ?? Object.keys(props);
      if (Object.keys(props).length === 0 && resolved.additionalProperties) {
        return { note: 'See schema for dynamic object fields.' };
      }
      for (const key of required) {
        if (props[key]) result[key] = sampleValue(props[key], components, key);
      }
      return result;
    }
    if (resolved.type === 'array') {
      return [sampleValue(resolved.items, components, keyName)];
    }
    return sampleScalar(resolved, keyName);
  }
  return null;
}

function mqttExampleValue(channel) {
  const mqttKey = channel.meta.tsKey;
  if (mqttKey && mqttExampleOverrides[mqttKey] !== undefined) {
    return mqttExampleOverrides[mqttKey];
  }
  return sampleValue(channel.payload, channel.components);
}

function mqttGroupIntro(group) {
  switch (group) {
    case 'Server topics':
      return 'Server-wide topics published independently of any specific printer.';
    case 'Printer topics':
      return 'Per-printer topics under `v1/devices/printers/{printerId}/data/...`.';
    case 'Per-job topics':
      return 'Topics scoped to a specific active or historical job under `.../jobs/{jobId}/data/...`.';
    case 'Slot topics':
      return 'Topics for multi-tool and MMU slot state, temperatures, and materials.';
    case 'Wildcard helpers':
      return 'Subscription helpers intended for clients; these contain MQTT wildcards and are not concrete publish channels.';
    default:
      return '';
  }
}

function sseGroupIntro(group) {
  switch (group) {
    case 'Printer lifecycle':
      return 'Events describing printer connection, disconnection, and generic printer-originated notifications.';
    case 'Printer state':
      return 'Events that explicitly describe state transitions of the printer state machine.';
    case 'Telemetry':
      return 'High-frequency or partial state deltas pushed to SSE clients.';
    case 'Jobs':
      return 'Events related to job lifecycle notifications.';
    case 'Transfers':
      return 'Events related to file transfer progress and status.';
    case 'Files':
      return 'Events describing file-system changes on the printer.';
    default:
      return 'Other SSE events that do not fit the main categories.';
  }
}

function mqttGroup(channel, meta) {
  if (meta.wildcard) return 'Wildcard helpers';
  if (channel.startsWith('v1/server/')) return 'Server topics';
  if (channel.includes('/jobs/')) return 'Per-job topics';
  if (channel.includes('/slot/')) return 'Slot topics';
  return 'Printer topics';
}

const mqttGroupOrder = ['Server topics', 'Printer topics', 'Per-job topics', 'Slot topics', 'Wildcard helpers'];

function mqttSortValue(channel) {
  const groupIndex = mqttGroupOrder.indexOf(channel.group);
  return [groupIndex === -1 ? 999 : groupIndex, channel.name];
}

// Topic names are machine identifiers, so they sort by code point, never by
// locale. String.localeCompare() follows the host locale, and e.g. Czech
// collation treats "ch" as one letter sorting after "h", which reorders
// .../temp/chamber/... against .../temp/heatbed/... The generated page would
// then differ between a developer's machine and CI, dirtying the tree on every
// docs build.
function compareTopicNames(a, b) {
  if (a === b) return 0;
  return a < b ? -1 : 1;
}

// Spec descriptions are free text written in Go, but they land in .mdx, where a
// brace opens a JSX expression and `<` opens a tag. An unescaped description
// therefore fails the docs build at render time rather than at generation. A
// summary containing "{id, code}" throws "ReferenceError: id is not defined".
// Escape every spec-derived string that is emitted as prose. Text inside code
// fences and inline backticks is exempt: MDX does not evaluate either.
function mdxText(value) {
  return String(value ?? '').replace(/[{}<]/g, (ch) => `\\${ch}`);
}

function markdownTable(headers, rows) {
  const header = `| ${headers.join(' | ')} |`;
  const divider = `| ${headers.map(() => '---').join(' | ')} |`;
  const body = rows.map((row) => `| ${row.join(' | ')} |`).join('\n');
  return [header, divider, body].filter(Boolean).join('\n');
}

function mqttWireExample(channel, example) {
  if (channel.meta.wildcard) {
    return `\`\`\`text
subscribe ${channel.name}
\`\`\``;
  }

  const payload = typeof example === 'string' ? example : JSON.stringify(example);
  return `\`\`\`text
${channel.name}
${payload}
\`\`\``;
}

function sseWireExample(channel, example) {
  return `\`\`\`text
event: ${channel.name}
data: ${JSON.stringify(example)}

\`\`\``;
}

function renderMqttPage(spec) {
  const channels = Object.entries(spec.channels ?? {}).map(([name, item]) => {
    const subscribe = item.subscribe ?? {};
    const payload = subscribe.message?.payload ?? {};
    const params = item.parameters ?? {};
    const tags = (subscribe.tags ?? []).map((t) => t.name).filter(Boolean);
    const meta = {
      tsKey: item['x-prusa-connect-ts-key'] ?? '',
      qos: item['x-prusa-connect-qos'] ?? 0,
      retain: item['x-prusa-connect-retain'] ?? false,
      wildcard: item['x-prusa-connect-wildcard'] ?? false,
      fieldMapping: item['x-prusa-connect-field-mapping'] ?? '',
    };
    return {
      name,
      summary: subscribe.summary ?? subscribe.message?.description ?? '',
      payload,
      params,
      tags,
      meta,
      group: mqttGroup(name, meta),
      components: spec.components,
    };
  });

  const groupOrder = mqttGroupOrder;
  const grouped = new Map(groupOrder.map((g) => [g, channels.filter((c) => c.group === g)]));
  const orderedChannels = [...channels].sort((a, b) => {
    const [ag, an] = mqttSortValue(a);
    const [bg, bn] = mqttSortValue(b);
    if (ag !== bg) return ag - bg;
    return compareTopicNames(an, bn);
  });

  let out = `---
title: MQTT topic reference
description: Generated native reference for MQTT topics, payloads, QoS, retain behavior, and wildcard helpers.
audience: run
rewrite:
  describes: current
  implementedBy:
    - adapters/events/mqtt
---

Generated by \`docs/scripts/generate-native-asyncapi-pages.mjs\` from \`adapters/events/mqtt/testdata/shipping-asyncapi.yaml\`. Do not edit: change the document and rebuild the site.

The \`v1/\` topic names, the payloads, the QoS and the retain flags below are a client-visible contract that carries over. The core publishes them from \`adapters/events/mqtt\`, an event sink that sends to a broker you already run, and \`TestTheTopicTableMatchesTheShippingAsyncAPIDocument\` holds its topic table against this document. What did not carry over is the embedded broker. See [MQTT overview](./mqtt-overview.md) for how to declare the sink.

## Servers

${markdownTable(['Name', 'Protocol', 'URL', 'Description'], Object.entries(spec.servers ?? {}).map(([name, server]) => [
  `\`${name}\``,
  `\`${server.protocol ?? ''}\``,
  `\`${server.url ?? ''}\``,
  mdxText(server.description ?? ''),
]))}

## Overview

${markdownTable(['Topic pattern', 'Summary', 'QoS', 'Retain', 'Tags'], orderedChannels.map((c) => [
  `\`${c.name}\``,
  mdxText(c.summary),
  `\`${c.meta.qos}\``,
  c.meta.retain ? '`true`' : '`false`',
  c.tags.length ? c.tags.map((t) => `\`${t}\``).join(', ') : 'none',
]))}
`;

  for (const group of groupOrder) {
    const items = grouped.get(group) ?? [];
    if (!items.length) continue;
    out += `\n## ${group}\n\n${mqttGroupIntro(group)}\n`;
    for (const c of items) {
      const example = mqttExampleValue(c);
      const payloadType = c.payload.enum
        ? `enum (${c.payload.enum.map((v) => `\`${v}\``).join(', ')})`
        : `\`${c.payload.type ?? 'object'}\``;
      out += `
### \`${c.name}\`

${mdxText(c.summary)}

- TS key: ${c.meta.tsKey ? `\`${c.meta.tsKey}\`` : 'none'}
- QoS: \`${c.meta.qos}\`
- Retain: ${c.meta.retain ? '`true`' : '`false`'}
- Wildcard helper: ${c.meta.wildcard ? '`true`' : '`false`'}
- Field mapping: ${c.meta.fieldMapping ? `\`${c.meta.fieldMapping}\`` : 'none'}
- Tags: ${c.tags.length ? c.tags.map((t) => `\`${t}\``).join(', ') : 'none'}

${Object.keys(c.params).length ? `#### Parameters

${markdownTable(
  ['Parameter', 'Type', 'Description'],
  Object.entries(c.params).map(([key, val]) => [
    `\`${key}\``,
    `\`${val.schema?.type ?? 'string'}\``,
    mdxText(val.description ?? ''),
  ]),
)}
` : ''}
#### Payload

- Type: ${payloadType}
${c.payload.description ? `- Description: ${mdxText(c.payload.description)}` : ''}

\`\`\`yaml
${formatYaml(c.payload)}
\`\`\`

#### Example payload

\`\`\`json
${formatJson(example)}
\`\`\`

#### Wire example

${mqttWireExample(c, example)}
`;
    }
  }

  return out;
}

function sseGroup(eventName, tags) {
  if (tags.includes('telemetry')) return 'Telemetry';
  if (tags.includes('transfer')) return 'Transfers';
  if (tags.includes('file')) return 'Files';
  if (tags.includes('job')) return 'Jobs';
  if (tags.includes('state')) return 'Printer state';
  if (tags.includes('printer')) return 'Printer lifecycle';
  return 'Other events';
}

function renderSsePage(spec) {
  const channels = Object.entries(spec.channels ?? {}).map(([name, item]) => {
    const subscribe = item.subscribe ?? {};
    const payload = subscribe.message?.payload ?? {};
    const tags = (subscribe.tags ?? []).map((t) => t.name).filter(Boolean);
    return {
      name,
      summary: subscribe.summary ?? subscribe.message?.description ?? '',
      payload,
      tags,
      group: sseGroup(name, tags),
      components: spec.components,
    };
  });

  const groupOrder = ['Printer lifecycle', 'Printer state', 'Telemetry', 'Jobs', 'Transfers', 'Files', 'Other events'];
  const grouped = new Map(groupOrder.map((g) => [g, channels.filter((c) => c.group === g)]));

  let out = `---
title: SSE event reference
description: Generated native reference for SSE event types, CloudEvent envelopes, and payload schemas.
audience: run
rewrite:
  describes: legacy
  implementedBy:
    - internal/arch/testdata/shipping-sse-asyncapi.yaml
    - internal/arch/testdata/legacy-sse-event-types.tsv
---

Generated by \`docs/scripts/generate-native-asyncapi-pages.mjs\` from the frozen AsyncAPI capture of the retired server. Do not edit: change that document and rebuild the site.

Every event on this page was served by the retired server at \`GET /api/events\`, under the \`prusa-connect.\` namespace. That server is deleted and nothing answers those names now. The core serves its own stream at \`GET /api/v1/events\`, under \`com.prusa.connect.\`, and its types are in [Event types](./event-types.md). This page is the frozen record of what the old stream carried: the names, the CloudEvents envelope and the \`types\` and \`printers\` filters are the half a rewrite has to honour, which is why the capture is gated.

## Servers

${markdownTable(['Name', 'Protocol', 'URL', 'Description'], Object.entries(spec.servers ?? {}).map(([name, server]) => [
  `\`${name}\``,
  `\`${server.protocol ?? ''}\``,
  `\`${server.url ?? ''}\``,
  mdxText(server.description ?? ''),
]))}

## Overview

${markdownTable(['Event type', 'Summary', 'Tags'], channels.map((c) => [
  `\`${c.name}\``,
  mdxText(c.summary),
  c.tags.length ? c.tags.map((t) => `\`${t}\``).join(', ') : 'none',
]))}
`;

  for (const group of groupOrder) {
    const items = grouped.get(group) ?? [];
    if (!items.length) continue;
    out += `\n## ${group}\n\n${sseGroupIntro(group)}\n`;
    for (const c of items) {
      const example = sseExampleOverrides[c.name] ?? sampleValue(c.payload, c.components);
      out += `
### \`${c.name}\`

${mdxText(c.summary)}

- Tags: ${c.tags.length ? c.tags.map((t) => `\`${t}\``).join(', ') : 'none'}

#### CloudEvent envelope schema

\`\`\`yaml
${formatYaml(c.payload)}
\`\`\`

#### Example event

\`\`\`json
${formatJson(example)}
\`\`\`

#### Event stream example

${sseWireExample(c, example)}
`;
    }
  }

  return out;
}

await rm(legacyAsyncApiDir, { recursive: true, force: true });

const mqttSpec = await readYaml(mqttSpecPath);
const sseSpec = await readYaml(sseSpecPath);

await mkdir(dirname(mqttPagePath), { recursive: true });
await mkdir(dirname(ssePagePath), { recursive: true });

await writeFile(mqttPagePath, renderMqttPage(mqttSpec), 'utf8');
await writeFile(ssePagePath, renderSsePage(sseSpec), 'utf8');
