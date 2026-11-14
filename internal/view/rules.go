package view

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

type rulesVM struct {
	list       ui.ListState
	selected   int
	editOpen   bool
	r          store.Rule
	action     string
	image      bool
	file       bool
	text       bool
	exts       string
	minSize    string
	maxSize    string
	minW, maxW string
	minH, maxH string
	errs       map[string]string
	defAction  string
	dryRun     []core.DryRunResult
	dryRunDone bool
	running    bool
}

func actionLabel(a store.Action) string {
	if a == store.ActionSkip {
		return locales.ActionSkip()
	}
	return locales.ActionUpload()
}

func actionOf(label string) store.Action {
	if label == locales.ActionSkip() {
		return store.ActionSkip
	}
	return store.ActionUpload
}

// mb formats a byte limit in MB for its field.
func mb(p *int64) string {
	if p == nil {
		return ""
	}
	return oneDecimal(float64(*p) / store.MB)
}

func parseMB(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, errors.New(locales.ErrNumber())
	}
	n := int64(f * store.MB)
	return &n, nil
}

func (vm *rulesVM) open(r store.Rule) {
	c := r.Conditions
	*vm = rulesVM{list: vm.list, selected: vm.selected, defAction: vm.defAction, dryRun: vm.dryRun, dryRunDone: vm.dryRunDone,
		editOpen: true, r: r, action: actionLabel(r.Action),
		image: slices.Contains(c.Kinds, store.KindImage), file: slices.Contains(c.Kinds, store.KindFile),
		text: slices.Contains(c.Kinds, store.KindText), exts: strings.Join(c.Extensions, ", "),
		minSize: mb(c.MinSize), maxSize: mb(c.MaxSize),
		minW: formatOptional(c.MinWidth), maxW: formatOptional(c.MaxWidth),
		minH: formatOptional(c.MinHeight), maxH: formatOptional(c.MaxHeight)}
}

// rule builds the edited rule, with the fields' own parse errors.
func (vm *rulesVM) rule() (store.Rule, map[string]string) {
	r := vm.r
	r.Action = actionOf(vm.action)
	errs := map[string]string{}
	c := store.Conditions{Glob: strings.TrimSpace(r.Conditions.Glob), TextRegex: r.Conditions.TextRegex}
	for _, k := range []struct {
		on   bool
		kind store.Kind
	}{{vm.image, store.KindImage}, {vm.file, store.KindFile}, {vm.text, store.KindText}} {
		if k.on {
			c.Kinds = append(c.Kinds, k.kind)
		}
	}
	for _, e := range strings.Split(vm.exts, ",") {
		if e = strings.TrimPrefix(strings.TrimSpace(e), "."); e != "" {
			c.Extensions = append(c.Extensions, e)
		}
	}
	var err error
	if c.MinSize, err = parseMB(vm.minSize); err != nil {
		errs[rules.FieldSize] = err.Error()
	}
	if c.MaxSize, err = parseMB(vm.maxSize); err != nil {
		errs[rules.FieldSize] = err.Error()
	}
	ints := func(s string, field string) *int {
		v, err := parseOptionalInt(s)
		if err != nil {
			errs[field] = err.Error()
			return nil
		}
		if v == nil {
			return nil
		}
		n := int(*v)
		return &n
	}
	c.MinWidth, c.MaxWidth = ints(vm.minW, rules.FieldWidth), ints(vm.maxW, rules.FieldWidth)
	c.MinHeight, c.MaxHeight = ints(vm.minH, rules.FieldHeight), ints(vm.maxH, rules.FieldHeight)
	r.Conditions = c
	if r.Action == store.ActionSkip {
		r.DirOverride, r.TemplateOverride = "", ""
	}
	return r, errs
}

// summary describes a rule's conditions in a line.
func ruleSummary(r store.Rule) string {
	c := r.Conditions
	var parts []string
	for _, k := range c.Kinds {
		parts = append(parts, kindLabel(k))
	}
	if len(c.Extensions) > 0 {
		parts = append(parts, "."+strings.Join(c.Extensions, ", ."))
	}
	if c.Glob != "" {
		parts = append(parts, c.Glob)
	}
	if c.MinSize != nil {
		parts = append(parts, "≥ "+HumanSize(*c.MinSize))
	}
	if c.MaxSize != nil {
		parts = append(parts, "≤ "+HumanSize(*c.MaxSize))
	}
	if c.MinWidth != nil || c.MaxWidth != nil || c.MinHeight != nil || c.MaxHeight != nil {
		parts = append(parts, formatOptional(c.MinWidth)+"–"+formatOptional(c.MaxWidth)+" × "+
			formatOptional(c.MinHeight)+"–"+formatOptional(c.MaxHeight)+" px")
	}
	if c.TextRegex != "" {
		parts = append(parts, "/"+c.TextRegex+"/")
	}
	if len(parts) == 0 {
		return locales.RuleAny()
	}
	return strings.Join(parts, " · ")
}

func (a *App) rulesPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.rules
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		Heading(c, locales.RulesTitle(), 24).Grow(1)
		if button(c, iconFlaskConical, locales.TestClipboard()).Disabled(vm.running).Clicked() {
			vm.running = true
			a.ctrl.DryRun(func(r []core.DryRunResult) { vm.dryRun, vm.dryRunDone, vm.running = r, true, false })
		}
		if primaryButton(c, iconPlus, locales.RulesAdd()).FontWeight(600).Clicked() {
			vm.open(store.Rule{Enabled: true, Action: store.ActionUpload})
		}
	})
	ui.Text(c, locales.RulesIntro()).TextColor(t.TextMuted)

	list := s.Rules
	vm.list.Key = func(i int) any { return list[i].ID }
	vm.list.Label = func(i int) string { return list[i].Name }
	vm.list.Reorder = func(rows []int, to int) {
		ids := make([]int64, 0, len(list))
		moving := map[int]bool{}
		for _, r := range rows {
			moving[r] = true
		}
		for i := 0; i <= len(list); i++ {
			if i == to {
				for _, r := range rows {
					ids = append(ids, list[r].ID)
				}
			}
			if i < len(list) && !moving[i] {
				ids = append(ids, list[i].ID)
			}
		}
		a.ctrl.ReorderRules(ids, nil)
	}
	ui.List(c, &vm.list, len(list), func(i int) {
		r := list[i]
		ui.Row(c).Gap(10).Padding(8, 10).AlignItems(ui.Center).Children(func() {
			// Rows reorder by dragging; the grip shows it.
			icon(c, iconGripVertical).TextColor(t.TextMuted)
			on := r.Enabled
			if ui.Switch(c, &on).Label(locales.RuleEnabled() + ": " + r.Name).Changed() {
				a.ctrl.SetRuleEnabled(r.ID, on, nil)
			}
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				name := ui.Text(c, r.Name).Bold()
				if !r.Enabled {
					name.TextColor(t.TextMuted)
				}
				ui.Text(c, actionLabel(r.Action)+" · "+ruleSummary(r)).TextColor(t.TextMuted).FontSize(12)
			})
			if iconButton(c, iconArrowUp, locales.MoveUp()+": "+r.Name, locales.MoveUp()).Disabled(i == 0).Clicked() {
				a.ctrl.MoveRule(r.ID, -1, nil)
			}
			if iconButton(c, iconArrowDown, locales.MoveDown()+": "+r.Name, locales.MoveDown()).Disabled(i == len(list)-1).Clicked() {
				a.ctrl.MoveRule(r.ID, 1, nil)
			}
			if iconButton(c, iconPencil, locales.Edit()+": "+r.Name, locales.Edit()).Clicked() {
				vm.open(r)
			}
			if iconButton(c, iconTrash, locales.Delete()+": "+r.Name, locales.Delete()).Clicked() {
				a.ctrl.DeleteRule(r.ID, nil)
			}
		})
	}).Label(locales.RulesTitle()).Height(float32(max(1, len(list)))*60).Border(1, t.Border).Radius(t.Radius)

	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		vm.defAction = actionLabel(s.Settings.DefaultAction)
		ui.Text(c, locales.RulesDefault())
		if ui.Select(c, &vm.defAction, []string{locales.ActionUpload(), locales.ActionSkip()}).
			Label(locales.RulesDefault()).Width(140).Changed() {
			a.ctrl.SetDefaultAction(actionOf(vm.defAction), nil)
		}
	})

	if vm.dryRunDone {
		a.dryRunPanel(c, t)
	}
	a.ruleDialog(c, t)
}

func (a *App) dryRunPanel(c *ui.Context, t *ui.Theme) {
	vm := &a.rules
	ui.Column(c).Gap(8).Padding(12).Radius(t.Radius).Background(t.Surface).Role(ui.RoleRegion).
		Label(locales.DryRunTitle()).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			icon(c, iconFlaskConical).TextColor(t.Accent)
			ui.Text(c, locales.DryRunTitle()).Bold()
		})
		if len(vm.dryRun) == 0 {
			ui.Text(c, locales.DryRunEmpty()).TextColor(t.TextMuted)
			return
		}
		cols := []ui.TableColumn{{Title: locales.ColEntry()}, {Title: locales.ColRule()}, {Title: locales.ColAction(), Width: 90},
			{Title: locales.ColRemotePath()}}
		ui.Table(c, nil, cols, len(vm.dryRun), func(row, col int) {
			r := vm.dryRun[row]
			switch col {
			case 0:
				ui.Text(c, r.Name)
			case 1:
				ui.Text(c, RuleText(r.RuleName))
			case 2:
				ui.Text(c, actionLabel(r.Action))
			case 3:
				if r.RemotePath != "" {
					ui.Text(c, r.RemotePath).TruncateMiddle()
				} else if r.Reason != "" {
					ui.Text(c, ReasonText(r.Reason)).TextColor(t.TextMuted)
				}
			}
		}).Height(float32(40 + 32*len(vm.dryRun)))
	})
}

func (a *App) ruleDialog(c *ui.Context, t *ui.Theme) {
	vm := &a.rules
	ui.Modal(c, &vm.editOpen, func() {
		title := locales.RuleEditTitle()
		if vm.r.ID == 0 {
			title = locales.RuleNewTitle()
		}
		Heading(c, title, 18)
		errOf := func(f string) string { return vm.errs[f] }
		field := func(label string, control func()) ui.Element {
			return leftAlignedField(c, 136, label, control)
		}
		ui.Scroll(c).MaxHeight(600).Children(func() {
			ui.Column(c).Gap(t.Space(3)).Children(func() {
				field(locales.FieldName(), func() { ui.TextInput(c, &vm.r.Name).AutoFocus().MaxWidth(300) }).
					Error(errOf(rules.FieldName))
				field(locales.FieldAction(), func() {
					ui.Select(c, &vm.action, []string{locales.ActionUpload(), locales.ActionSkip()}).MaxWidth(140)
				})
				ui.Fieldset(c, locales.FieldKinds(), func() {
					ui.Row(c).Gap(12).Children(func() {
						ui.Checkbox(c, &vm.image, locales.KindImages())
						ui.Checkbox(c, &vm.file, locales.KindFiles())
						ui.Checkbox(c, &vm.text, locales.KindText())
					})
				})
				field(locales.FieldExtensions(), func() { ui.TextInput(c, &vm.exts).MaxWidth(300) }).
					Description(locales.ExtensionsHint())
				field(locales.FieldGlob(), func() { ui.TextInput(c, &vm.r.Conditions.Glob).MaxWidth(300) }).
					Description(locales.GlobHint()).Error(errOf(rules.FieldGlob))
				// Each range is one row: minimum to maximum.
				rng := func(label, minLabel, maxLabel string, lo, hi *string, key string) {
					field(label, func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.TextInput(c, lo).Grow(1).MaxWidth(100).Label(minLabel)
							ui.Text(c, locales.RangeTo()).TextColor(t.TextMuted)
							ui.TextInput(c, hi).Grow(1).MaxWidth(100).Label(maxLabel)
						})
					}).Error(errOf(key))
				}
				rng(locales.FieldSize(), locales.FieldMinSize(), locales.FieldMaxSize(), &vm.minSize, &vm.maxSize, rules.FieldSize)
				rng(locales.FieldWidth(), locales.FieldMinWidth(), locales.FieldMaxWidth(), &vm.minW, &vm.maxW, rules.FieldWidth)
				rng(locales.FieldHeight(), locales.FieldMinHeight(), locales.FieldMaxHeight(), &vm.minH, &vm.maxH, rules.FieldHeight)
				field(locales.FieldRegex(), func() { ui.TextInput(c, &vm.r.Conditions.TextRegex).MaxWidth(300) }).
					Description(locales.RegexHint()).Error(errOf(rules.FieldRegex))
				if actionOf(vm.action) == store.ActionUpload {
					field(locales.FieldDirOverride(), func() { ui.TextInput(c, &vm.r.DirOverride).MaxWidth(300) }).
						Description(locales.DirOverrideHint()).Error(errOf(rules.FieldDir))
					field(locales.FieldTemplateOverride(), func() { ui.TextInput(c, &vm.r.TemplateOverride).MaxWidth(300) }).
						Description(locales.TemplateOverrideHint()).Error(errOf(rules.FieldTemplate))
				}
			})
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Cancel()).Clicked() {
				vm.editOpen = false
			}
			if ui.PrimaryButton(c, locales.Save()).FontWeight(600).Clicked() {
				r, errs := vm.rule()
				vm.errs = errs
				if len(errs) == 0 {
					a.ctrl.SaveRule(r, func(err error) {
						var re *rules.RuleError
						switch {
						case errors.As(err, &re):
							vm.errs = map[string]string{}
							for _, f := range re.Fields {
								vm.errs[f.Field] = ErrorText(f.Err)
							}
						case err != nil:
							vm.errs = map[string]string{rules.FieldName: locales.SomethingWrong(err.Error())}
						default:
							vm.editOpen = false
						}
					})
				}
			}
		})
	}).Width(560)
}
