// The questions and their menus. Every answer also has a flag. A question's
// default is the flag's value or the answer from the previous pass. A summary
// asks for confirmation before anything is written.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// asker asks questions on out and reads one-line answers from in. After in
// ends, every question takes its default and the write menu cancels.
type asker struct {
	in    *bufio.Reader
	out   io.Writer
	ended bool
	color bool
}

// isTerminal reports whether in is a character device. /dev/null is one too
// and is excluded: nohup, docker without -i and CI runners give it as stdin.
func isTerminal(in *os.File) bool {
	if in == nil {
		return false
	}

	info, err := in.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	devNull, err := os.Stat(os.DevNull)

	return err != nil || !os.SameFile(info, devNull)
}

// The ANSI codes the questions are styled with.
const (
	styleBold  = "\x1b[1m"
	styleDim   = "\x1b[2m"
	styleGreen = "\x1b[32m"
	styleCyan  = "\x1b[36m"
	styleReset = "\x1b[0m"
)

func (a *asker) paint(style, text string) string { return paint(a.color, style, text) }

// question is a question's text and a one-line hint about what the answer is
// for.
type question struct{ text, hint string }

func (a *asker) show(q question, options []string) {
	if a.ended {
		return
	}

	fmt.Fprintf(a.out, "\n%s\n%s\n", a.paint(styleBold, "? "+q.text), a.paint(styleDim, "  "+q.hint))

	for number, option := range options {
		fmt.Fprintf(a.out, "  %d) %s\n", number+1, option)
	}
}

// showGrouped is show with the options under one heading per group. The
// numbers run on from 1 across all groups.
func (a *asker) showGrouped(q question, groups [][]string, headings []string) {
	if a.ended {
		return
	}

	fmt.Fprintf(a.out, "\n%s\n%s\n", a.paint(styleBold, "? "+q.text), a.paint(styleDim, "  "+q.hint))

	number := 1

	for index, group := range groups {
		fmt.Fprintf(a.out, "  %s\n", a.paint(styleDim, headings[index]))

		for _, option := range group {
			fmt.Fprintf(a.out, "  %d) %s\n", number, option)
			number++
		}
	}
}

// read prompts once and returns the answer. An empty answer or an ended
// stream returns fallback.
func (a *asker) read(fallback string) string {
	if a.ended {
		return fallback
	}

	fmt.Fprint(a.out, a.paint(styleCyan, "›")+" ")

	if fallback != "" {
		fmt.Fprint(a.out, a.paint(styleDim, "["+fallback+"]")+" ")
	}

	typed, err := a.in.ReadString('\n')
	if err != nil && strings.TrimSpace(typed) == "" {
		a.ended = true
		fmt.Fprintln(a.out)

		return fallback
	}

	return firstOf(typed, fallback)
}

// confirm prints the accepted answer.
func (a *asker) confirm(answer string) {
	if a.ended {
		return
	}

	fmt.Fprintf(a.out, "%s %s\n", a.paint(styleGreen, "✔"), answer)
}

// line asks q once and accepts any answer.
func (a *asker) line(q question, fallback string) string {
	a.show(q, nil)

	answer := a.read(fallback)
	a.confirm(answer)

	return answer
}

// printable makes typed safe to print. A terminal that erased half of a
// multibyte character leaves bytes that are not UTF-8.
func printable(typed string) string { return strings.ToValidUTF8(typed, "\uFFFD") }

// offered is the part of a menu option before the colon.
func offered(option string) string {
	words, _, _ := strings.Cut(option, ":")

	return words
}

// label asks q until the answer is a valid resource name. The answer ends up
// in routes and ids.
func (a *asker) label(q question, fallback string) string {
	a.show(q, nil)

	for {
		answer := a.read(fallback)
		if _, err := ref.New(ref.Kind(initKind), answer); err == nil {
			a.confirm(answer)

			return answer
		}

		if a.ended {
			return fallback
		}

		fmt.Fprintf(a.out, "  \"%s\" is not a short name. Use lower-case letters, digits and dashes, "+
			"like nozzle-watch.\n", printable(answer))
	}
}

// yes asks a yes or no question. Enter takes fallback.
func (a *asker) yes(q question, fallback bool) bool {
	a.show(q, nil)

	shown := "y/N"
	if fallback {
		shown = "Y/n"
	}

	for {
		if a.ended {
			return fallback
		}

		switch strings.ToLower(a.read(shown)) {
		case strings.ToLower(shown):
		case "y", "yes":
			fallback = true
		case "n", "no":
			fallback = false
		default:
			fmt.Fprintln(a.out, "  Answer y or n.")

			continue
		}

		a.confirm(map[bool]string{true: "yes", false: "no"}[fallback])

		return fallback
	}
}

// choose asks for one option by number, counting from 1. Enter takes
// fallback. Any other answer is asked again.
func (a *asker) choose(q question, options []string, fallback int) int {
	a.show(q, options)

	for {
		picked, err := numbersOf(a.read(strconv.Itoa(fallback)), len(options))
		if err == nil && len(picked) == 1 {
			a.confirm(offered(options[picked[0]-1]))

			return picked[0]
		}

		if a.ended {
			return fallback
		}

		fmt.Fprintf(a.out, "  Type one number from 1 to %d.\n", len(options))
	}
}

// chooseMany asks for any number of options, as numbers separated by commas.
// Enter takes fallback.
func (a *asker) chooseMany(q question, options []string, fallback []int) []int {
	a.show(q, options)

	return a.readMany(options, fallback)
}

// chooseManyGrouped is chooseMany with the options printed in named groups.
func (a *asker) chooseManyGrouped(q question, groups [][]string, headings []string, fallback []int) []int {
	var options []string
	for _, group := range groups {
		options = append(options, group...)
	}

	a.showGrouped(q, groups, headings)

	return a.readMany(options, fallback)
}

// readMany reads a list of numbers until it is valid. The options are
// already on the screen.
func (a *asker) readMany(options []string, fallback []int) []int {
	written := make([]string, 0, len(fallback))
	for _, number := range fallback {
		written = append(written, strconv.Itoa(number))
	}

	for {
		picked, err := numbersOf(a.read(strings.Join(written, ",")), len(options))
		if err == nil {
			words := make([]string, 0, len(picked))
			for _, number := range picked {
				words = append(words, offered(options[number-1]))
			}

			a.confirm(strings.Join(words, "; "))

			return picked
		}

		if a.ended {
			return fallback
		}

		fmt.Fprintf(a.out, "  %v.\n", err)
	}
}

// repository asks for the repository address until it gives a namespace, and
// prints that namespace.
func (a *asker) repository(fallback string) string {
	a.show(questions["repo"], nil)

	for {
		typed := repoOf(a.read(fallback))

		namespace, err := extension.NamespaceOf(typed)
		if err == nil {
			a.confirm(typed)
			fmt.Fprintf(a.out, "  Its namespace will be: %s\n", namespace)

			return typed
		}

		if a.ended {
			return fallback
		}

		fmt.Fprintf(
			a.out,
			"  %s gives no namespace. Type a host and a path, like github.com/you/nozzle-watch.\n",
			typed,
		)
	}
}

func askKind(a *asker, current shape) shape {
	return shapes[a.choose(questions["kind"], shapeOffers(), shapeNumber(current))-1]
}

// numbersOf reads numbers separated by commas, each from 1 to most. It
// returns them sorted and without duplicates.
func numbersOf(typed string, most int) ([]int, error) {
	var picked []int

	for field := range strings.SplitSeq(typed, ",") {
		field = strings.TrimSpace(field)

		number, err := strconv.Atoi(field)
		if err != nil || number < 1 || number > most {
			return nil, fmt.Errorf("\"%s\" is not a list of numbers from 1 to %d", printable(typed), most)
		}

		if !slices.Contains(picked, number) {
			picked = append(picked, number)
		}
	}

	slices.Sort(picked)

	return picked, nil
}

// questions is every question and its hint, by key.
//
//nolint:gochecknoglobals // a closed table, read at the prompt and never written.
var questions = map[string]question{
	"kind": {"What kind of module is it?",
		"A UI adds something to the app's screens. A process is a program the server runs for the module."},
	"name": {
		"What is its short name?",
		"Lower-case letters, digits and dashes, like nozzle-watch. It names the folder, the files and the address of its page.",
	},
	"repo": {
		"Where will its code be published?",
		"A repository address, like github.com/you/nozzle-watch. The server makes the module's namespace from it: the unique name all its addresses start with.",
	},
	"adds": {
		"What does it add to the app?",
		"Type one or more numbers, separated by commas, like 1,3. Each one is a place in the app the module may draw in, and module.yaml lists them under contributes.",
	},
	"memory": {
		"How much memory does its process need, in MiB?",
		"The server gives a module 128 MiB unless module.yaml recommends more, and stops it over its budget. " +
			"Leave it empty for 128, or type a figure from 16 to 8192 that covers its busiest moment, child programs included.",
	},
	"dir": {"Where should its folder go?",
		"A path from here. The folder must not exist yet, or must be empty."},
	"git": {"Where should its code live?",
		"git records every change to the module's code, and a repository on a server keeps a copy of it."},
	"remote": {
		"What is the repository's address?",
		"Paste the address of an empty repository, like git@github.com:you/nozzle-watch.git. It is checked before anything is written.",
	},
	"advanced": {"Change the title, the page address or the ids?",
		"Most modules keep the defaults. The command printed at the end writes the title you change, " +
			"and the defaults for the addresses and ids."},
	"advanced-process": {"Change the title?", "Most modules keep the defaults."},
	"title":            {"Its title", "What people read wherever the app names this module."},
	"page-id":          {"The page's address", "The last part of the page's URL: one label, like nozzle-watch."},
	"page-title":       {"The page's title", "What people read in the sidebar and at the top of the page."},
	"section-id":       {"Its id", "A label the app tells this one apart by. People never see it."},
	"section-title":    {"Its title", "What people read on it."},
	"action-id":        {"Its id", "A label the app tells this one apart by. People never see it."},
	"action-title":     {"Its title", "What people read on the menu item."},
	"icon": {
		"Its icon",
		"One of the icons the app ships, or the library's default.",
	},
	"destructive": {
		"Draw it as a destructive item?",
		"The host asks no confirmation; the module confirms it if it needs to.",
	},
	"write": {"Write it?", "Nothing is written until you choose 1."},
	"install": {
		"Build it and install it now?",
		"This runs sh install.sh. It builds the module and installs it on a demo server. The first run takes a few minutes.",
	},
	"start": {
		"Start it now?",
		"A server starts on a free port with the module installed, and for a UI its dev server in front of it. Ctrl+C stops them.",
	},
}

// interview asks every question after the kind. A flag that was given is
// not asked about.
func interview(ctx context.Context, a *asker, given map[string]bool, request answers) answers {
	if !given["name"] {
		request.name = a.label(questions["name"], request.name)
	}

	name := strings.TrimSpace(request.name)
	if name == "" {
		// planFor reports the empty name. Nothing more is worth asking.
		return request
	}

	fmt.Fprintf(a.out, "  Its title will be: %s\n", titleOf(request.title, name))

	if !given["repo"] {
		// Only a typed answer is stored. The default stays unset, so it
		// follows the name when the name changes on a later pass.
		typed := a.repository(firstOf(request.repo, repoDefault(request.login, name)))
		request.repo = ""

		if typed != repoDefault(request.login, name) {
			request.repo = typed
		}
	}

	if request.shape.draws() && !given["contributes"] {
		request.contributions = slots(a, request.contributions)
	}

	if request.shape.runs() && !given["memory-mib"] {
		request.memoryMiB = memory(a)
	}

	if !given["dir"] {
		request = where(a, request, name)
	}

	if !given["git"] && !given["remote"] && !given["github"] {
		request = codeHome(ctx, a, request, name)
	}

	advancedKey := "advanced-process"
	if request.shape.draws() {
		advancedKey = "advanced"
	}

	if a.yes(questions[advancedKey], false) {
		request = advanced(a, given, request, name)
	}

	return request
}

// memory asks for the process's memory in MiB. It returns 0 for an empty
// answer and after two answers outside the manifest's bounds.
func memory(a *asker) int {
	for range 2 {
		answer := strings.TrimSpace(a.line(questions["memory"], ""))
		if answer == "" {
			return 0
		}

		figure, err := strconv.Atoi(answer)
		if err == nil && figure >= manifest.MinMemoryMiB && figure <= manifest.MaxMemoryMiB {
			return figure
		}

		fmt.Fprintf(
			a.out,
			"  Type a whole number from %d to %d, or nothing.\n",
			manifest.MinMemoryMiB,
			manifest.MaxMemoryMiB,
		)
	}

	return 0
}

// slots asks which slots the bundle fills, grouped by screen. The default is
// the answers so far, or the first slot when there are none.
func slots(
	a *asker, held map[manifest.SlotName]scaffold.Contribution,
) map[manifest.SlotName]scaffold.Contribution {
	var (
		groups   [][]string
		headings []string
		flat     []manifest.SlotName
	)

	for _, screen := range manifest.Screens() {
		var offers []string

		for _, slot := range manifest.Slots() {
			if slot.Screen != screen {
				continue
			}

			offers = append(offers, slot.Offer)
			flat = append(flat, slot.Name)
		}

		if len(offers) == 0 {
			continue
		}

		groups = append(groups, offers)
		headings = append(headings, screen.Doc())
	}

	fallback := []int{}

	for index, name := range flat {
		if _, makes := held[name]; makes {
			fallback = append(fallback, index+1)
		}
	}

	if len(fallback) == 0 {
		fallback = []int{1}
	}

	chosen := make(map[manifest.SlotName]scaffold.Contribution, len(flat))

	for _, number := range a.chooseManyGrouped(questions["adds"], groups, headings, fallback) {
		name := flat[number-1]
		chosen[name] = held[name]
	}

	return chosen
}

// labelQuestions returns the keys of the id and title questions for row's
// form. A card uses the section's pair.
func labelQuestions(row manifest.Slot) (id, title string) {
	switch row.Form {
	case manifest.FormPage:
		return "page-id", "page-title"
	case manifest.FormAction:
		return "action-id", "action-title"
	case manifest.FormSection, manifest.FormCard:
		return "section-id", "section-title"
	default:
		return "section-id", "section-title"
	}
}

// forSlot appends row.Offer to q, so a shared question names its slot.
func forSlot(q question, row manifest.Slot) question {
	q.text += ": " + row.Offer

	return q
}

// iconOffers is the icon menu: the library's default, then manifest.Icons in
// order.
func iconOffers() []string {
	offers := make([]string, 0, len(manifest.Icons())+1)
	offers = append(offers, "the library's default")

	for _, icon := range manifest.Icons() {
		offers = append(offers, string(icon))
	}

	return offers
}

// defaultIconNumber is the menu number of "the library's default".
const defaultIconNumber = 1

// iconNumber is icon's number in iconOffers, or the default's for an empty
// or unknown icon.
func iconNumber(icon manifest.Icon) int {
	if index := slices.Index(manifest.Icons(), icon); index >= 0 {
		return defaultIconNumber + 1 + index
	}

	return defaultIconNumber
}

// askContribution asks for one contribution's id and title, plus an icon for
// a page or action. Only a typed answer is stored, so defaults follow the name.
func askContribution(
	a *asker,
	row manifest.Slot,
	made scaffold.Contribution,
	name, title string,
) scaffold.Contribution {
	idKey, titleKey := labelQuestions(row)
	idQuestion, titleQuestion := questions[idKey], questions[titleKey]

	if row.Form != manifest.FormPage {
		idQuestion, titleQuestion = forSlot(idQuestion, row), forSlot(titleQuestion, row)
	}

	typedID := a.label(idQuestion, firstOf(made.ID, name))
	made.ID = ""

	if typedID != name {
		made.ID = typedID
	}

	typedTitle := a.line(titleQuestion, firstOf(made.Title, title))
	made.Title = ""

	if typedTitle != title {
		made.Title = typedTitle
	}

	if row.Form == manifest.FormPage || row.Form == manifest.FormAction {
		picked := a.choose(questions["icon"], iconOffers(), iconNumber(made.Icon))
		made.Icon = ""

		if picked > 1 {
			made.Icon = manifest.Icons()[picked-2]
		}
	}

	if row.Form == manifest.FormAction {
		made.Destructive = a.yes(questions["destructive"], made.Destructive)
	}

	return made
}

// advanced asks for the title and then the details of each contribution.
// Without it they all take defaults.
func advanced(a *asker, given map[string]bool, request answers, name string) answers {
	if !given["title"] {
		typed := a.line(questions["title"], titleOf(request.title, name))
		request.title = ""

		if typed != titleOf("", name) {
			request.title = typed
		}
	}

	if !request.shape.draws() {
		return request
	}

	title := titleOf(request.title, name)

	for _, row := range manifest.Slots() {
		made, makes := request.contributions[row.Name]
		if !makes {
			continue
		}

		request.contributions[row.Name] = askContribution(a, row, made, name, title)
	}

	return request
}

// errCancelled means the person cancelled at the summary. Nothing was written.
var errCancelled = errors.New("cancelled")

// The menu numbers of two answers at the summary.
const (
	answerWrite  = 1
	answerCancel = 3
)

// settle asks until the person confirms or cancels. Tools are checked right
// after the kind. A later pass asks every question again, flags included.
func settle(
	ctx context.Context, a *asker, env environment, given map[string]bool, request answers,
) (answers, scaffold.Plan, error) {
	for {
		if !given["kind"] {
			request.shape = askKind(a, request.shape)
		}

		if err := checkTools(ctx, request.shape, env); err != nil {
			return request, scaffold.Plan{}, err
		}

		request = interview(ctx, a, given, request)

		plan, err := planFor(request)
		if err != nil {
			return request, scaffold.Plan{}, err
		}

		summarise(a.out, request, plan)

		picked := a.choose(questions["write"],
			[]string{"yes, write it", "no, change the answers", "no, cancel"}, answerWrite)

		// An ended stream never confirms a write, whatever the default is.
		if a.ended {
			return request, scaffold.Plan{}, errCancelled
		}

		switch picked {
		case answerWrite:
			return request, plan, nil
		case answerCancel:
			return request, scaffold.Plan{}, errCancelled
		}

		given = map[string]bool{}
	}
}

// where asks for the module's folder until it is free. Only a typed answer
// is stored, so the default follows the name on a later pass.
func where(a *asker, request answers, name string) answers {
	fallback := firstOf(request.dir, defaultDir(request, name))

	a.show(questions["dir"], nil)

	for {
		request.dir = ""
		typed := a.read(fallback)
		if typed != defaultDir(request, name) {
			request.dir = typed
		}

		_, dir, err := placeFor(request, name)
		if err != nil {
			return request
		}

		err = scaffold.Vacant(dir)
		if err == nil {
			a.confirm(typed)
			fmt.Fprintf(a.out, "  That is %s\n", dir)

			return request
		}

		if a.ended {
			return request
		}

		fmt.Fprintf(a.out, "  %v\n", err)
	}
}
