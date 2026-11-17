---
title: "ADR-20261001-pdr: a listing says how it may be filtered"
description: "The fleet listing answer lists the filters it offers with their values and counts, the sorts it offers, and the order it applied, so a client shows only what the server advertises."
date: 2026-10-01
status: accepted
amends: [ADR-20260917-j7s]
tags:
  - adr
  - api
  - frontend
---
# ADR-20261001-pdr: a listing says how it may be filtered

- status: accepted
- date: 2026-10-01

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20260917-j7s](20260917-j7s-a-screen-takes-a-port-and-a-listing-is-one-kit.md) | ADRs: [20260827-xca](20260827-xca-a-fleet-says-what-is-wrong-with-it.md)

The printers screen would otherwise read the server's default page of 50
devices and invent its filter chips and their counts from those rows. In a
fleet larger than a page the values and counts are wrong, and nothing in the
API says which filters exist. A client order by state tier is one the server
cannot reproduce, so a sorted page and a sorted fleet disagree.

## rules

1. `GET /api/v1/devices` answers with `filters[]`, in toolbar order `search`,
   `state`, `model`, `severity`, `tag`. Each has `param`, `kind` (`multi` or
   `text`), `values[{value, count}]` for `multi`, and `max_length` for `text`.
2. A `multi` value is listed when any printer the caller may see, after the
   `retired` clause, has it. A value the request chose is listed even when no
   printer has it, and `severity` always lists its three values. The count is
   `0` when the other filters exclude every printer that has the value.
3. A value's count applies every other clause and not its own dimension, so a
   chip shows what a click would bring. `tag` is the exception: the tag filter
   is an AND, so its own clause stays applied and the count is the printers
   with every chosen tag and this one.
4. The answer has `sorts[]` with `default_order`, and echoes the applied
   `sort` and `order`.
5. `urgency` is the default sort. Its tiers are `ERROR`, `ATTENTION`,
   `PAUSED`/`FINISHED`/`STOPPED`, `OFFLINE`/`UNKNOWN`/unrecognised or not
   connected, then the working states. Ties break by ascending device id in
   both directions.
6. `severity` is a filter, and `issues.by_severity` counts by it. The issue
   rollup counts every printer the caller may see that is not retired, whatever
   the request's filters say. `retired=only` or `retired=include` does not
   change it, and a retired printer is never in it.
7. No label is sent on the wire. A client names a state, a model or a severity
   from the vocabularies and its own i18n.
8. ADR-20260917-j7s rule 7 still holds, but the server computes the counts for the
   printers screen. The screen shows the chips the answer advertises and does
   not count them itself.

## enforcement

`TestCommittedSpecMatchesTheCode` and `make openapi-check api-check sdk-check`
check the shape. Tests in `internal/app` check the counts.

## rejected

| option | why not |
|---|---|
| keep inventing facets in the client | wrong beyond one page, and a new filter is two changes that drift |
| send display labels with the values | the server would own translation, and the vocabularies already name them |
| keep the client's state-tier order | the server cannot page by it, so page two would not follow page one |
