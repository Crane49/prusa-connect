// Which module exported a component (ADR-20261029-zzg). A module's styles
// reach only its own markup, so a component drawn inside another module's
// page is wrapped in its owner's `data-module`, and its styles come with it.
// The registry records the owner when the module registers the component;
// every bundle shares this one library, so one map serves the page.
import type { Component } from 'svelte';

// Every component, whatever its props, is a Component<never>.
type AnyComponent = Component<never>;

const owners = new WeakMap<AnyComponent, string>();

/** Records the namespace of the module that registered a component. */
export const ownedBy = (component: AnyComponent, namespace: string): void => {
  owners.set(component, namespace);
};

/** The namespace of the module that registered a component, if one did. */
export const ownerOf = (component: AnyComponent): string | undefined =>
  owners.get(component);
