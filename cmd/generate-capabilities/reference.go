package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/internal/kernel/hostfeature"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	frontMatter = "---\ntitle: Capability vocabulary\n" +
		"description: Every capability key the shipped printer profiles declare, " +
		"the arguments each verb takes, the value kinds an argument may carry, and " +
		"the features the host promises a module.\n" +
		"audience: extend\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - sdk/connectorapi\n" +
		"    - internal/printer/profile\n    - internal/kernel/hostfeature\n---\n\n"

	preamble = "A driver declares what its code can do in `Baseline()`, as capability keys. This page is the " +
		"vocabulary those keys are drawn from: what the printers this product already drives declare, and " +
		"what each argument accepts. The path from nothing to a driver that declares them is " +
		"[writing a printer driver](./writing-a-driver.md), and what the host promises in return is " +
		"[the connector contract](./connector-contract.md).\n\n" +
		"The key set is not closed by a gate. `sdk/connectorapi` checks a key's syntax and nothing checks " +
		"that a key means something, so a driver may declare a key that is not here and it will resolve, " +
		"validate and reach `Do` like any other. What it will not do is reach a caller that knows what to " +
		"send: the API, the frontend and every profile in this tree speak the keys below. A dialect that " +
		"needs a verb nothing here names is a change to this vocabulary, not a private extension of it.\n\n" +
		"This page lists the keys the shipped profiles declare. Another driver may name an argument " +
		"differently, such as `celsius` for a nozzle target, and narrows each range per machine. What one " +
		"device accepts is `GET /api/v1/devices/{device_id}/capabilities`: every verb it resolved to, and " +
		"for each argument its name, type, range, default, closed set of values and unit. The unit is one " +
		"of `celsius`, `percent`, `mm`, `mm/min`, `s` and `byte`, and the driver declares it.\n\n" +
		"A key whose meaning changes gets a new name and the old one is retired in " +
		"a list the profile package holds, which is refused for good. No schema language detects a " +
		"field that kept its name and changed what it means, so the retirement list is the mechanism.\n"
)

// kindMeaning says what each value kind means. Every kind in the closed set
// needs a row.
//
//nolint:gochecknoglobals // a package-level table read by one renderer; there is nowhere cheaper to put it.
var kindMeaning = map[connectorapi.Kind]string{
	connectorapi.KindFlag:    "no value. It belongs to a key that answers only \"can you\", such as a verb or a firmware feature",
	connectorapi.KindString:  "free text, bounded by the limit when one is declared",
	connectorapi.KindInteger: "a whole number",
	connectorapi.KindNumber:  "a fractional number",
	connectorapi.KindBoolean: "`true` or `false`, in those spellings and no other",
	connectorapi.KindOpaque:  "a value the host passes through untouched, such as an embedded JSON document",
}

// toolMeaning is what the tool argument of the nozzle commands names.
const toolMeaning = "the head, counted from 0 as the head ids under `/tools` in the printer's state are: " +
	"the first head is 0"

// argumentMeaning says what an argument means where its name and kind do not.
// Every row must name a key a shipped profile declares.
//
//nolint:gochecknoglobals // a package-level table read by one renderer, as kindMeaning is.
var argumentMeaning = map[connectorapi.Capability]string{
	"core.command.set_nozzle_temperature#tool":   toolMeaning + ". Without it, the active head",
	"core.command.set_tool_hardened#tool":        toolMeaning,
	"core.command.set_tool_high_flow#tool":       toolMeaning,
	"core.command.set_tool_nozzle_diameter#tool": toolMeaning,
}

func renderReference(registry profile.Registry) []byte {
	var page strings.Builder

	coverage := registry.Coverage()

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeContract(&page)
	writeKinds(&page)
	writeVerbs(&page, registry, coverage)
	writeFeatures(&page, registry, coverage)
	writeHostFeatures(&page)
	writeModels(&page, registry)

	return []byte(page.String())
}

func writeContract(page *strings.Builder) {
	page.WriteString("\n## Contract version\n\n")
	fmt.Fprintf(page, "This build serves connector contract `%s`. A driver declares the version it compiled "+
		"against in its descriptor's `Contract` field, and a host serving another major refuses it at "+
		"registration rather than at first use.\n", connectorapi.Contract)
}

func writeKinds(page *strings.Builder) {
	page.WriteString("\n## Value kinds\n\n")
	page.WriteString("Arguments travel as text. The kind is what tells the host how to read one before a " +
		"driver sees it.\n\n")

	for _, kind := range kinds() {
		fmt.Fprintf(page, "- `%s` %s\n", kind, docsref.EscapeProse(kindMeaning[kind]))
	}
}

// kinds returns the closed set in declaration order.
func kinds() []connectorapi.Kind {
	out := make([]connectorapi.Kind, 0, len(kindMeaning))
	for kind := connectorapi.KindFlag; kind.Valid(); kind++ {
		out = append(out, kind)
	}

	return out
}

// writeVerbs renders one anchored section per verb with its arguments nested
// under it. An argument means nothing away from its verb.
func writeVerbs(page *strings.Builder, registry profile.Registry, coverage connectorapi.Set) {
	page.WriteString("\n## Commands\n\n")
	page.WriteString("A verb accepts exactly the arguments for which the descriptor carries an argument key. " +
		"There is no wildcard: an argument nothing declares is refused by the host before a driver is " +
		"reached, so a verb is subtracted by leaving its key out and an argument the same way.\n\n")
	page.WriteString("`limit` is an inclusive bound on the value's magnitude: the absolute value of a " +
		"number, the length in bytes of a string. `min` is an inclusive lower bound on a number itself, " +
		"which is what says that a target may not go negative. The values below are the loosest form any " +
		"shipped profile gives the key, and one printer's own descriptor may be narrower.\n")

	for _, verb := range group(coverage, "core.command.") {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", verb, docsref.Anchor(verb.String()))
		fmt.Fprintf(page, "- **arguments** %s\n", argumentList(coverage, verb))
		fmt.Fprintf(page, "- **declared by** %s\n", declaredBy(registry, verb))

		writeArgumentsOf(page, coverage, verb)
	}
}

// writeArgumentsOf gives each argument its own anchor so a page can link to it.
func writeArgumentsOf(page *strings.Builder, coverage connectorapi.Set, verb connectorapi.Capability) {
	for _, key := range coverage.Keys() {
		name, isArgument := key.Argument()
		if !isArgument || key.Verb() != verb || !coverage.Has(key) {
			continue
		}

		value := coverage[key]

		fmt.Fprintf(page, "\n#### `%s` {#%s}\n\n", name, docsref.Anchor(key.String()))
		fmt.Fprintf(page, "- **key** `%s`\n", docsref.EscapeSpan(key.String()))

		if meaning := argumentMeaning[key]; meaning != "" {
			fmt.Fprintf(page, "- **meaning** %s\n", docsref.EscapeProse(meaning))
		}

		fmt.Fprintf(page, "- **kind** `%s`\n", value.Kind)
		fmt.Fprintf(page, "- **required** %s\n", docsref.YesNo(value.Required))
		fmt.Fprintf(page, "- **default** %s\n", docsref.OrNone(value.Default))
		fmt.Fprintf(page, "- **min** %s\n", bound(value.Min, value.HasMin))
		fmt.Fprintf(page, "- **limit** %s\n", bound(value.Limit, value.HasLimit))
		fmt.Fprintf(page, "- **supported** %s\n", supportedList(value.Supported))
	}
}

func writeFeatures(page *strings.Builder, registry profile.Registry, coverage connectorapi.Set) {
	features := group(coverage, "core.feature.")
	if len(features) == 0 {
		return
	}

	page.WriteString("\n## Features\n\n")
	page.WriteString("A feature carries no value and no argument. It answers whether the printer behaves a " +
		"certain way, which is what a caller branches on instead of on a firmware version.\n\n")
	page.WriteString("| feature | declared by |\n| --- | --- |\n")

	for _, key := range features {
		fmt.Fprintf(page, "| `%s` | %s |\n", key, declaredBy(registry, key))
	}
}

// writeHostFeatures renders the keys the host promises a module. The table
// lists every key and the builds that have it, not only this build's keys.
func writeHostFeatures(page *strings.Builder) {
	features := hostfeature.Declared()
	if len(features) == 0 {
		return
	}

	page.WriteString("\n## Host features\n\n")
	page.WriteString("A host feature is a key the server declares about itself. A module names one under " +
		"`requires` in its manifest, and a host that carries no such key refuses the module at install " +
		"rather than starting it against something that is not there.\n\n")
	page.WriteString("| feature | carried by | what it promises |\n| --- | --- | --- |\n")

	for _, feature := range features {
		fmt.Fprintf(page, "| `%s` | %s | %s |\n", feature.Key, feature.Builds,
			docsref.EscapeProse(feature.Summary))
	}
}

func writeModels(page *strings.Builder, registry profile.Registry) {
	page.WriteString("\n## Profiles\n\n")
	page.WriteString("The profile tier narrows a driver's baseline for a model running a firmware range. " +
		"A printer no profile covers is held to the union of them all, so an unrecognised device is never " +
		"offered more than a recognised one.\n\n")
	page.WriteString("| model | firmware from | firmware to | keys |\n| --- | --- | --- | --- |\n")

	for _, entry := range registry.Profiles {
		fmt.Fprintf(page, "| `%s` | `%s` | %s | %d |\n",
			docsref.EscapeSpan(entry.Model), docsref.EscapeSpan(entry.Firmware.From),
			orOpen(entry.Firmware.To), len(entry.Capabilities))
	}
}

// group returns the enabled verb and feature keys with the prefix, sorted.
// Argument keys are left out.
func group(set connectorapi.Set, prefix string) []connectorapi.Capability {
	var out []connectorapi.Capability

	for _, key := range set.Keys() {
		if _, isArgument := key.Argument(); isArgument || !set.Has(key) {
			continue
		}

		if strings.HasPrefix(key.String(), prefix) {
			out = append(out, key)
		}
	}

	return out
}

// argumentList names the arguments one verb takes.
func argumentList(set connectorapi.Set, verb connectorapi.Capability) string {
	var names []string

	for _, key := range set.Keys() {
		name, isArgument := key.Argument()
		if !isArgument || key.Verb() != verb || !set.Has(key) {
			continue
		}

		names = append(names, "`"+docsref.EscapeSpan(name)+"`")
	}

	if len(names) == 0 {
		return docsref.None
	}

	return strings.Join(names, ", ")
}

// declaredBy says how many profiles declare the key, as "3 of 7".
func declaredBy(registry profile.Registry, key connectorapi.Capability) string {
	count := 0

	for _, entry := range registry.Profiles {
		if entry.Capabilities.Has(key) {
			count++
		}
	}

	return strconv.Itoa(count) + " of " + strconv.Itoa(len(registry.Profiles))
}

func supportedList(supported []string) string {
	if len(supported) == 0 {
		return "any"
	}

	values := slices.Clone(supported)
	slices.Sort(values)

	for i, value := range values {
		values[i] = "`" + docsref.EscapeSpan(value) + "`"
	}

	return strings.Join(values, ", ")
}

func bound(value float64, has bool) string {
	if !has {
		return docsref.None
	}

	return "`" + strconv.FormatFloat(value, 'g', -1, 64) + "`"
}

func orOpen(value string) string {
	if value == "" {
		return "open"
	}

	return "`" + docsref.EscapeSpan(value) + "`"
}
