// Moved from pull request 185, packages/screens/src/actions-context.ts,
// 2026-09-11
//
// Where a product's overrides are read.
//
// A context and not a prop on every screen: a control deep in a tree would
// otherwise be threaded through everything above it, and that scaffolding
// stays in every file long after.
//
// There is no fallback for a missing context, unlike a slot that is simply not
// filled. Every action reaches the printer through the client the application
// built, so an action set assembled without one would be a set of buttons that
// quietly do nothing.
//
// An application calls this once, above its own tree, so there is one
// context. A product that wants different actions under part of its tree
// calls `setCommandActions` again there.
import { getContext, setContext } from 'svelte';

import type { CommandActions } from './actions';

const COMMAND_ACTIONS = Symbol('prusa.command-actions');

/** Called once, above every screen that draws a lifecycle control. */
export const setCommandActions = (actions: CommandActions): CommandActions =>
  setContext(COMMAND_ACTIONS, actions);

export const getCommandActions = (): CommandActions => {
  const actions = getContext<CommandActions | undefined>(COMMAND_ACTIONS);
  if (!actions) {
    throw new Error(
      'getCommandActions was called outside a component under setCommandActions',
    );
  }

  return actions;
};
