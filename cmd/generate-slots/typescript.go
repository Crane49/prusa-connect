package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// tsIdentifier matches a name that needs no quotes as an object key.
var tsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// prettierWidth is the column prettier wraps at. The output is wrapped the
// same way, so it needs no formatting pass.
const prettierWidth = 80

// tsKey returns name as an object key: bare when it is an identifier, quoted
// otherwise.
func tsKey(name string) string {
	if tsIdentifier.MatchString(name) {
		return name
	}

	return "'" + name + "'"
}

// tsLit returns name as a string literal, always quoted.
func tsLit(name string) string { return "'" + name + "'" }

// tsPascal capitalises each hyphen-separated word: chart-line becomes
// ChartLine, the name of the lucide icon import.
func tsPascal(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}

	return strings.Join(parts, "")
}

// tsUnion writes a union of string literals, wrapped as prettier wraps it:
// first after `=`, then one member per line. An empty set is `never`.
func tsUnion(name string, members []string) string {
	if len(members) == 0 {
		return fmt.Sprintf("export type %s = never;\n", name)
	}

	literals := make([]string, len(members))
	for i, m := range members {
		literals[i] = tsLit(m)
	}
	joined := strings.Join(literals, " | ")

	inline := fmt.Sprintf("export type %s = %s;", name, joined)
	if len(inline) <= prettierWidth {
		return inline + "\n"
	}

	broken := "  " + joined + ";"
	if len(broken) <= prettierWidth {
		return fmt.Sprintf("export type %s =\n%s\n", name, broken)
	}

	var lines strings.Builder
	for _, l := range literals {
		fmt.Fprintf(&lines, "  | %s\n", l)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "export type %s =\n", name)
	b.WriteString(strings.TrimSuffix(lines.String(), "\n"))
	b.WriteString(";\n")

	return b.String()
}

// tsSlotsOfForm returns the slots of one form, in table order.
func tsSlotsOfForm(slots []manifest.Slot, form manifest.Form) []manifest.Slot {
	var out []manifest.Slot

	for _, slot := range slots {
		if slot.Form == form {
			out = append(out, slot)
		}
	}

	return out
}

// tsSectionType is a section slot's prop type, from its subject.
func tsSectionType(subject manifest.Subject) string {
	if prop := subject.Prop(); prop != "" {
		return fmt.Sprintf("{ %s: string }", prop)
	}

	return "Record<string, never>"
}

// tsRegister describes one slot's ModuleRegister method, its buildRegister
// property and the RegisterHandlers method that property calls.
type tsRegister struct {
	// paramList is the ModuleRegister method's typed parameters.
	paramList []string
	// args is the same parameters as bare names.
	args string
	// calls is the RegisterHandlers method to forward to. callArgs is its
	// arguments.
	calls, callArgs string
	// optional marks a RegisterHandlers method a host may leave out. It is
	// called with `?.`.
	optional bool
}

func tsRegisterOf(slot manifest.Slot) tsRegister {
	switch slot.Form {
	case manifest.FormPage:
		return tsRegister{
			paramList: []string{"path: string", "component: Component"},
			args:      "path, component", calls: "page", callArgs: "path, component",
		}
	case manifest.FormSection:
		return tsRegister{
			paramList: []string{
				"id: string",
				fmt.Sprintf("component: Component<SectionProps[%s]>", tsLit(string(slot.Name))),
			},
			args: "id, component", calls: "section",
			callArgs: fmt.Sprintf("%s, id, component", tsLit(string(slot.Name))),
		}
	case manifest.FormAction:
		return tsRegister{
			paramList: []string{"id: string", fmt.Sprintf("run: ActionRuns[%s]", tsLit(string(slot.Name)))},
			args:      "id, run", calls: "action",
			callArgs: fmt.Sprintf("%s, id, run", tsLit(string(slot.Name))),
		}
	case manifest.FormCard:
		return tsRegister{
			paramList: []string{"id: string", "component: Component<CardProps>", "options?: CardOptions"},
			args:      "id, component, options", calls: "card",
			callArgs: "id, component, options", optional: true,
		}
	default:
		return tsRegister{}
	}
}

// tsWrapCall writes one interface member, `prefix(<params>): void;`. It
// puts one parameter per line when the member is wider than prettierWidth.
func tsWrapCall(b *strings.Builder, prefix string, params []string) {
	const (
		indent = "  "
		suffix = ": void;"
	)

	inline := indent + prefix + "(" + strings.Join(params, ", ") + ")" + suffix
	if len(inline) <= prettierWidth {
		fmt.Fprintf(b, "%s\n", inline)

		return
	}

	fmt.Fprintf(b, "%s%s(\n", indent, prefix)
	for _, p := range params {
		fmt.Fprintf(b, "%s  %s,\n", indent, p)
	}

	fmt.Fprintf(b, "%s)%s\n", indent, suffix)
}

// libraryIcons maps an icon name to the library primitive drawn in place of a
// lucide glyph, so a module shows the same icon as the core's navigation.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var libraryIcons = map[manifest.Icon]string{
	"materials": "MaterialsGlyphIcon",
	"printer":   "PrinterGlyphIcon",
}

// renderTypeScript writes the library's slot contract: every type and value
// packages/svelte/src/slots imports from ./generated.
func renderTypeScript(slots []manifest.Slot, icons []manifest.Icon) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", generatedNotice)
	b.WriteString("import type { Component } from 'svelte';\n")
	b.WriteString("import type { LucideIcon } from '@lucide/svelte';\n")

	for _, icon := range icons {
		if _, own := libraryIcons[icon]; !own {
			fmt.Fprintf(&b, "import %sIcon from '@lucide/svelte/icons/%s';\n", tsPascal(string(icon)), icon)
		}
	}

	for _, icon := range icons {
		if component, own := libraryIcons[icon]; own {
			fmt.Fprintf(&b, "import { %s as %sIcon } from '../primitives';\n", component, tsPascal(string(icon)))
		}
	}

	b.WriteString("import type { CardOptions, CardProps } from './card-types';\n")
	b.WriteString("import type { ModuleContributes } from './types';\n\n")

	tsWriteSlotNames(&b, slots)
	tsWriteScreen(&b)
	tsWriteSectionAndActionSlots(&b, slots)
	tsWriteSlotKeys(&b, slots)
	tsWriteSlotScreens(&b, slots)
	tsWriteSlotForms(&b, slots)
	tsWriteSlotsOf(&b)
	tsWriteSectionProps(&b, slots)
	tsWriteActionRuns(&b, slots)
	tsWriteSectionOutletTarget(&b, slots)
	tsWriteActionOutletTarget(&b, slots)
	tsWriteRegisterHandlers(&b)
	tsWriteModuleRegister(&b, slots)
	tsWriteBuildRegister(&b, slots)
	tsWriteIcon(&b, icons)
	tsWriteModuleIcons(&b, icons)

	return []byte(b.String())
}

// tsWriteSlotNames writes SLOT_NAMES and SlotName. The sidebar is added by
// hand: it follows from a page and has no row in the table.
func tsWriteSlotNames(b *strings.Builder, slots []manifest.Slot) {
	names := make([]string, 0, len(slots)+1)
	names = append(names, "sidebar")
	for _, slot := range slots {
		names = append(names, string(slot.Name))
	}

	literals := make([]string, len(names))
	for i, n := range names {
		literals[i] = tsLit(n)
	}

	inline := fmt.Sprintf("export const SLOT_NAMES = [%s] as const;", strings.Join(literals, ", "))
	if len(inline) <= prettierWidth {
		b.WriteString(inline + "\n")
	} else {
		b.WriteString("export const SLOT_NAMES = [\n")
		for _, l := range literals {
			fmt.Fprintf(b, "  %s,\n", l)
		}

		b.WriteString("] as const;\n")
	}

	b.WriteString("export type SlotName = (typeof SLOT_NAMES)[number];\n")
}

func tsWriteScreen(b *strings.Builder) {
	screens := manifest.Screens()
	names := make([]string, len(screens))

	for i, screen := range screens {
		names[i] = string(screen)
	}

	b.WriteString(tsUnion("Screen", names))
}

func tsWriteSectionAndActionSlots(b *strings.Builder, slots []manifest.Slot) {
	sections := tsSlotsOfForm(slots, manifest.FormSection)
	actions := tsSlotsOfForm(slots, manifest.FormAction)

	sectionNames := make([]string, len(sections))
	for i, s := range sections {
		sectionNames[i] = string(s.Name)
	}

	actionNames := make([]string, len(actions))
	for i, a := range actions {
		actionNames[i] = string(a.Name)
	}

	b.WriteString(tsUnion("SectionSlot", sectionNames))
	b.WriteString(tsUnion("ActionSlot", actionNames))
	b.WriteString("\n")
}

// tsSatisfiesRecord writes `} as const satisfies Record<key, value>;`,
// wrapped as prettier wraps it.
func tsSatisfiesRecord(key, value string) string {
	inline := fmt.Sprintf("} as const satisfies Record<%s, %s>;", key, value)
	if len(inline) <= prettierWidth {
		return inline + "\n\n"
	}

	return fmt.Sprintf("} as const satisfies Record<\n  %s,\n  %s\n>;\n\n", key, value)
}

// tsWriteSlotKeys writes SLOT_KEYS, the manifest key of each section and
// action slot. ActionSlot is named only when an action slot exists.
func tsWriteSlotKeys(b *strings.Builder, slots []manifest.Slot) {
	actions := tsSlotsOfForm(slots, manifest.FormAction)

	b.WriteString("export const SLOT_KEYS = {\n")

	for _, slot := range append(tsSlotsOfForm(slots, manifest.FormSection), actions...) {
		fmt.Fprintf(b, "  %s: %s,\n", tsKey(string(slot.Name)), tsLit(slot.Name.Key()))
	}

	keyType := "SectionSlot"
	if len(actions) > 0 {
		keyType = "SectionSlot | ActionSlot"
	}

	b.WriteString(tsSatisfiesRecord(keyType, "keyof ModuleContributes"))
}

// tsWriteSlotScreens writes SLOT_SCREENS, the screen of every slot. The
// sidebar is always on the app screen.
func tsWriteSlotScreens(b *strings.Builder, slots []manifest.Slot) {
	b.WriteString("export const SLOT_SCREENS = {\n")
	fmt.Fprintf(b, "  sidebar: %s,\n", tsLit("app"))

	for _, slot := range slots {
		fmt.Fprintf(b, "  %s: %s,\n", tsKey(string(slot.Name)), tsLit(string(slot.Screen)))
	}

	b.WriteString(tsSatisfiesRecord("SlotName", "Screen"))
}

// tsFormLiteral is the SLOT_FORMS value of each form.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var tsFormLiteral = map[manifest.Form]string{
	manifest.FormPage:    "page",
	manifest.FormSection: "section",
	manifest.FormAction:  "action",
	manifest.FormCard:    "card",
}

// tsWriteSlotForms writes SLOT_FORMS, the form of each slot. The sidebar is
// 'derived' because it follows from a page registration.
func tsWriteSlotForms(b *strings.Builder, slots []manifest.Slot) {
	b.WriteString("export const SLOT_FORMS = {\n")
	fmt.Fprintf(b, "  sidebar: %s,\n", tsLit("derived"))

	for _, slot := range slots {
		fmt.Fprintf(b, "  %s: %s,\n", tsKey(string(slot.Name)), tsLit(tsFormLiteral[slot.Form]))
	}

	b.WriteString(tsSatisfiesRecord("SlotName", "'page' | 'section' | 'action' | 'card' | 'derived'"))
}

func tsWriteSlotsOf(b *strings.Builder) {
	b.WriteString("export type SlotsOf<S extends Screen> = {\n")
	b.WriteString("  [K in SlotName]: (typeof SLOT_SCREENS)[K] extends S ? K : never;\n")
	b.WriteString("}[SlotName];\n\n")
}

func tsWriteSectionProps(b *strings.Builder, slots []manifest.Slot) {
	b.WriteString("export interface SectionProps {\n")

	for _, slot := range tsSlotsOfForm(slots, manifest.FormSection) {
		fmt.Fprintf(b, "  %s: %s;\n", tsKey(string(slot.Name)), tsSectionType(slot.Subject))
	}

	b.WriteString("}\n\n")
}

// tsWriteActionRuns writes ActionRuns, the run function of each action slot.
// With no action slot the interface is empty and needs an eslint waiver.
func tsWriteActionRuns(b *strings.Builder, slots []manifest.Slot) {
	actions := tsSlotsOfForm(slots, manifest.FormAction)
	if len(actions) == 0 {
		b.WriteString(
			"// eslint-disable-next-line @typescript-eslint/no-empty-object-type -- filled by the action slots the table lists\n",
		)
		b.WriteString("export interface ActionRuns {}\n\n")

		return
	}

	b.WriteString("export interface ActionRuns {\n")

	for _, slot := range actions {
		fmt.Fprintf(b, "  %s: (%s: string) => Promise<void>;\n", tsKey(string(slot.Name)), slot.Subject.Prop())
	}

	b.WriteString("}\n\n")
}

// tsWriteSectionOutletTarget writes one member per section slot: its screen,
// its name and the id of its subject when it has one.
func tsWriteSectionOutletTarget(b *strings.Builder, slots []manifest.Slot) {
	sections := tsSlotsOfForm(slots, manifest.FormSection)

	var members strings.Builder

	for _, slot := range sections {
		member := fmt.Sprintf("{ screen: %s; name: %s", tsLit(string(slot.Screen)), tsLit(string(slot.Name)))
		if prop := slot.Subject.Prop(); prop != "" {
			member += fmt.Sprintf("; %s: string", prop)
		}

		member += " }"
		fmt.Fprintf(&members, "  | %s\n", member)
	}

	b.WriteString("export type SectionOutletTarget =\n")
	b.WriteString(strings.TrimSuffix(members.String(), "\n"))
	b.WriteString(";\n\n")
}

// tsWriteActionOutletTarget writes one member per action slot: its name and
// the id of its subject. With no action slot the type is `never`.
func tsWriteActionOutletTarget(b *strings.Builder, slots []manifest.Slot) {
	actions := tsSlotsOfForm(slots, manifest.FormAction)
	if len(actions) == 0 {
		b.WriteString("export type ActionOutletTarget = never;\n\n")

		return
	}

	members := make([]string, len(actions))

	for i, slot := range actions {
		member := "{ name: " + tsLit(string(slot.Name))
		if prop := slot.Subject.Prop(); prop != "" {
			member += fmt.Sprintf("; %s: string", prop)
		}

		members[i] = member + " }"
	}

	inline := fmt.Sprintf("export type ActionOutletTarget = %s;", strings.Join(members, " | "))
	if len(members) == 1 && len(inline) <= prettierWidth {
		b.WriteString(inline + "\n\n")

		return
	}

	b.WriteString("export type ActionOutletTarget =\n")

	for i, member := range members {
		b.WriteString("  | " + member)
		if i == len(members)-1 {
			b.WriteString(";")
		}

		b.WriteString("\n")
	}

	b.WriteString("\n")
}

// tsWriteRegisterHandlers writes RegisterHandlers. A card takes no slot
// argument because only one slot is a card. `card` and `component` are optional.
func tsWriteRegisterHandlers(b *strings.Builder) {
	b.WriteString("export interface RegisterHandlers {\n")
	tsWrapCall(b, "page", []string{"path: string", "component: Component"})
	tsWrapCall(b, "section<S extends SectionSlot>",
		[]string{"slot: S", "id: string", "component: Component<SectionProps[S]>"})
	tsWrapCall(b, "action<S extends ActionSlot>",
		[]string{"slot: S", "id: string", "run: ActionRuns[S]"})
	tsWrapCall(b, "card?",
		[]string{"id: string", "component: Component<CardProps>", "options?: CardOptions"})
	tsWrapCall(b, "component?", []string{"name: string", "component: Component<never>"})
	b.WriteString("}\n\n")
}

// tsWriteModuleRegister writes ModuleRegister: one method per slot, with the
// slot's Where as its doc comment.
func tsWriteModuleRegister(b *strings.Builder, slots []manifest.Slot) {
	b.WriteString("/** What a bundle calls to fill the slots its manifest declares. */\n")
	b.WriteString("export interface ModuleRegister {\n")

	for _, slot := range slots {
		reg := tsRegisterOf(slot)
		fmt.Fprintf(b, "  /** %s */\n", slot.Where)
		tsWrapCall(b, slot.Name.Register(), reg.paramList)
	}

	// A component is not a slot: other bundles draw it.
	b.WriteString(
		"  /** Hands a component to other modules' bundles, under a name ui.exports in the manifest names. */\n",
	)
	tsWrapCall(b, "component", []string{"name: string", "component: Component<never>"})
	b.WriteString("}\n\n")
}

// tsWriteBuildRegister writes buildRegister: one property per slot, each
// calling the RegisterHandlers method of its form.
func tsWriteBuildRegister(b *strings.Builder, slots []manifest.Slot) {
	b.WriteString("export const buildRegister = (handlers: RegisterHandlers): ModuleRegister => ({\n")

	for _, slot := range slots {
		reg := tsRegisterOf(slot)
		fmt.Fprintf(b, "  %s: (%s) => {\n", slot.Name.Register(), reg.args)
		call := reg.calls
		if reg.optional {
			call += "?."
		}

		fmt.Fprintf(b, "    handlers.%s(%s);\n", call, reg.callArgs)
		b.WriteString("  },\n")
	}

	b.WriteString("  component: (name, component) => {\n")
	b.WriteString("    handlers.component?.(name, component);\n")
	b.WriteString("  },\n")
	b.WriteString("});\n\n")
}

// tsWriteIcon writes Icon, the icons a manifest may name.
func tsWriteIcon(b *strings.Builder, icons []manifest.Icon) {
	names := make([]string, len(icons))
	for i, icon := range icons {
		names[i] = string(icon)
	}

	b.WriteString(tsUnion("Icon", names))
	b.WriteString("\n")
}

func tsWriteModuleIcons(b *strings.Builder, icons []manifest.Icon) {
	b.WriteString("export const MODULE_ICONS: Readonly<Record<Icon, LucideIcon>> = {\n")

	for _, icon := range icons {
		fmt.Fprintf(b, "  %s: %sIcon,\n", tsKey(string(icon)), tsPascal(string(icon)))
	}

	b.WriteString("};\n")
}
