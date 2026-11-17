# Engineering standards

How to look things up, where a decision is written down, and how to check work before reporting it. Package boundaries, meaning which package may import which, are in [ADR-20260824-kkr](../../docs/decisions/20260824-kkr-repository-layout-and-package-boundaries.md).

## Look it up before writing

Look in this order:

1. the exact error message in quotes, following links only to the project's own issues, source or docs
2. the official docs page, not a tutorial and not a blog summary
3. the project changelog since the installed version
4. the source, if the docs do not answer it: the function signature, the test that exercises the path, the comment above the option
5. recent git history for context

Prefer primary sources: official docs, the project's own GitHub, named engineering blogs, peer-reviewed papers, RFCs and standards bodies. Skip Medium posts, "top 10" lists, AI-generated blogs, vendor sales pages, Stack Overflow answers with no link to the docs, and tutorials older than the tool's last major version.

The user takes the informed choice. Research it, present the evidence, recommend the option that is best for the codebase over the long term, and let them decide.

If you think "this is the easiest approach" without doing the above, you are guessing.

## Where a decision is written down

The code, the ADRs and the GitHub issues are the sources
of truth. Nothing else is kept.

| kind | where |
|---|---|
| a decision that shapes the code, such as a new top-level directory or a change to what `sdk/` exports | an ADR in `docs/decisions/`, from `make adr-new` |
| a question the maintainers have to answer | a GitHub issue |
| follow-up work or known debt that is worth tracking | a GitHub issue |
| a measurement, a library choice, a version pin with a reason, a workaround that is not obvious | the ADR it supports, or the commit body and the pull request description |
| behaviour a user would notice | a fragment in `docs/changes/`, in the same commit |

A decision that exists only in a conversation did not happen. If the code shows
an ADR is wrong, say so with the evidence and write a new ADR that supersedes
or amends it.

## A spawned agent does not spawn agents

An agent given a task does that task itself. It does not delegate, does not
start other agents, and does not start background work of its own.

- the orchestrator, the one agent that starts the others, is the only thing
  that spawns agents, because it is the only thing that can see the total cost
- an agent that starts other agents multiplies token spend where nobody can
  see it, and can use up the rate limit of the whole session
- such an agent also fails when it has to combine the results: it collects
  more than it can hold and returns something unusable

Every agent prompt states this. A task too large for one agent goes back to the
orchestrator to be split.

## The map stays current

`docs/map.md` says where things are and what is built. A change that adds a
package, moves one, adds a check or finishes something on its status table
updates the map in the same commit. If the map disagrees with the tree, the
tree is right and the map is a bug.

## Present candidates before implementing

A choice that is not trivial (a new library, a new service, a refactor, a storage engine) is never made silently. Research it, then give two or three candidates with one sentence and one trade-off each. Name your pick and why, and let the user redirect.

Skip this only for small scope, or when the user already picked, or when an ADR already decided it.

## Fix the root cause across all environments

A bug is fixed everywhere it appears, not only in the layer you are working in. Do not dismiss an older bug as "not my problem" if it blocks the current work. "We'll fix it later" is not acceptable: fix it now, or open a tracked issue with an owner.

## Think first, then code

A change across several files gets a written design and a list of side effects (cache, performance, observability, schema, config, downstream) before the first edit.

## Tests live with the code

Write the tests in the same commit as the behaviour they test. A commit that adds behaviour without a test may be wrong, and the next person cannot tell what it was supposed to do.

Exception: a spike, meaning a quick experiment marked as one. Its test comes when the spike is promoted to real code.

## Verify: a logical check, then a mechanical one

This section exists to prevent one failure. The agent writes code, re-reads the code it wrote, predicts what that code will do, and reports "it works" without running it.

**Prediction is not verification.** Code can compile, type-check, pass static analysis and still be wrong, because none of those tools watched it run. You have verified nothing until a tool call in this session returned the actual bytes, rows, status codes or pixels you are claiming.

If the truth is "I wrote it, I believe it works, I have not run it", say that. Do not present a prediction as a result.

### The logical check

Answer each of these in writing. Writing the answer into a message catches the cases where you "kind of know" but have not checked.

1. re-read the original request. Does what I built do that, not approximately, but exactly?
2. trace one real user story from start to end. At each step, does the behaviour match what a user would expect? If anything looks like "that can't be right", it is not right
3. separate observed from assumed. List every claim you are about to carry into "done". Mark each one **observed** (a tool call this session returned that exact result and you read it) or **assumed** (you reasoned it from the code). Every assumption that matters must become an observation before you report done
4. what does the test not cover? Empty, zero-length, huge, concurrent on shared state, malformed, timeout mid-write, network error after a partial write, clock skew, negative, unicode, nil, permission-denied
5. compare to the rest of the codebase. Does this look like how similar things are done here, or did I invent a competing pattern?
6. what would a reviewer name first? Write it down, and fix it before the mechanical pass

A failed logical check always beats a passed mechanical check. Green tests on wrong behaviour are worse than red tests on right behaviour.

### The mechanical check

A green test is a hypothesis. Confirm it with independent observations, meaning tool output you read, not your reasoning about what the output would be.

- compute the expected value with a different query than the runtime uses. If the test and the runtime share the query, the test proves nothing
- call the actual endpoint and quote the actual status and one field value
- scan logs for ERROR, WARN, rolled back, retry. Exit 0 is not the same as no silent failures
- diff before and after for any state change
- one regression check on something unrelated that worked an hour ago

If the stack is not up, start it. "I'll check later" is not done.

## Test end-to-end like a user

Before reporting done, use the thing the way a user would: call the endpoint, run the binary with real flags, drive the emulator, read the response with your eyes. If you cannot (the service will not run, the hardware is not there), say so plainly instead of reporting a unit test as end-to-end.

Forbidden when reporting done: "it should work", "it'll take effect shortly", "the test passed so it's fine".
