package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/pacificbelt30/codex-dock/internal/policyevents"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/pacificbelt30/codex-dock/internal/workeregress"
	"github.com/rivo/tview"
)

type idleLayoutJobs struct{ desktopJobFixture }

func (*idleLayoutJobs) JobLogs(ctx context.Context, _, _ string, _ int64) (sandbox.JobOutput, error) {
	<-ctx.Done()
	return sandbox.JobOutput{}, ctx.Err()
}

func TestDesktopJobActionsFitAndRemainReachableAfterResize(t *testing.T) {
	d, app, _ := testDesktop(t)
	d.backend.Jobs = &idleLayoutJobs{}
	worker := sandbox.Worker{ID: "worker", Name: "長い名前-[red]-worker", Status: "running"}
	d.showJobOutput(worker, sandbox.Job{ID: strings.Repeat("b", 64)}, &sandbox.Job{ID: strings.Repeat("a", 64), ExitCode: 23}, func() {})
	_, page := d.pages.GetFrontPage()
	body := page.(*tview.Flex)
	view, form := body.GetItem(0).(*tview.TextView), body.GetItem(2).(*tview.Form)
	view.SetText("Readable retained output")
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	d.jobOutput(worker, sandbox.Job{ID: "ordinary-job"})
	_, ordinary := d.pages.GetFrontPage()
	screen.SetSize(24, 10)
	ordinary.SetRect(0, 0, 24, 10)
	ordinary.Draw(screen)
	screen.Show()
	if !strings.Contains(screenText(screen), "Back") || strings.Contains(screenText(screen), "Prev log") {
		t.Fatal("ordinary output must label Back correctly at the minimum size")
	}
	d.pages.AddAndSwitchToPage("job-logs", body, true)
	app.SetFocus(view)
	sizes := [][2]int{{150, 48}, {80, 24}, {48, 16}, {40, 12}, {32, 12}, {24, 10}, {80, 12}, {150, 48}}
	for width := 24; width <= 160; width++ {
		for _, height := range []int{10, 12, 24, 48} {
			sizes = append(sizes, [2]int{width, height})
		}
	}
	for _, size := range sizes {
		screen.SetSize(size[0], size[1])
		screen.Clear()
		body.SetRect(0, 0, size[0], size[1])
		body.Draw(screen)
		screen.Show()
		for i := range form.GetButtonCount() {
			button := form.GetButton(i)
			x, y, w, h := button.GetRect()
			if x < 0 || y < 0 || w < tview.TaggedStringWidth(button.GetLabel()) || x+w > size[0] || y+h > size[1] ||
				!strings.Contains(screenText(screen), button.GetLabel()) {
				t.Fatalf("button %q missing or clipped at %v", button.GetLabel(), size)
			}
		}
		if !strings.Contains(screenText(screen), "Readable") {
			t.Fatalf("actions hid the output at %v", size)
		}
	}
	key := func(key tcell.Key) {
		body.InputHandler()(tcell.NewEventKey(key, 0, 0), func(p tview.Primitive) { app.SetFocus(p) })
	}
	app.SetFocus(view)
	for _, step := range []struct {
		key   tcell.Key
		focus tview.Primitive
	}{
		{tcell.KeyTab, form.GetButton(0)},
		{tcell.KeyTab, form.GetButton(2)},
		{tcell.KeyBacktab, form.GetButton(0)},
		{tcell.KeyBacktab, view},
		{tcell.KeyBacktab, form.GetButton(3)},
		{tcell.KeyTab, view},
	} {
		key(step.key)
		if app.GetFocus() != step.focus {
			t.Fatalf("focus got stuck after %v", step.key)
		}
	}
}

func TestDesktopDialogsKeepLongTextAndControlsReachable(t *testing.T) {
	d, app, _ := testDesktop(t)
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(24, 10)
	accepted, cancelled := false, false
	d.confirmation("First line\n"+strings.Repeat("Long command line\n", 28)+"LAST ARG 日本語", func() { accepted = true }, func() { cancelled = true })
	_, body := d.pages.GetFrontPage()
	body.SetRect(0, 0, 24, 10)
	draw := func() {
		screen.Clear()
		body.Draw(screen)
		screen.Show()
	}
	draw()
	for _, want := range []string{"First line", "Cancel", "Confirm"} {
		if !strings.Contains(screenText(screen), want) {
			t.Fatal("short terminal hid", want)
		}
	}
	key := func(key tcell.Key) {
		body.InputHandler()(tcell.NewEventKey(key, 0, 0), func(p tview.Primitive) { app.SetFocus(p) })
	}
	for range 12 {
		key(tcell.KeyPgDn)
		draw()
	}
	var visible strings.Builder
	for y := range 10 {
		for x := 0; x < 24; {
			cell, _, width := screen.Get(x, y)
			visible.WriteString(cell)
			x += max(width, 1)
		}
		visible.WriteByte('\n')
	}
	if !strings.Contains(visible.String(), "LAST ARG 日本語") || !strings.Contains(visible.String(), "Confirm") {
		t.Fatal("end of review or action is inaccessible", visible.String())
	}
	key(tcell.KeyEscape)
	if !cancelled || accepted {
		t.Fatal("review did not cancel safely")
	}
}

func TestTerminalSizeGuardKeepsReviewsSafeAtExtremeSizes(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	root := tview.NewFlex()
	app := tview.NewApplication()
	guardTerminalSize(app, root)
	for _, size := range [][2]int{{1, 1}, {12, 3}, {23, 20}, {30, 9}, {24, 10}, {150, 50}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root.SetRect(0, 0, size[0], size[1])
			screen.SetSize(size[0], size[1])
			drawn := drawTerminalSizeNotice(screen)
			small := size[0] < 24 || size[1] < 10
			if drawn != small || (app.GetInputCapture()(tcell.NewEventKey(tcell.KeyEnter, 0, 0)) == nil) != small {
				t.Fatal("visible bounds and confirmation guard disagree")
			}
			if mouse, _ := app.GetMouseCapture()(tcell.NewEventMouse(1, 1, tcell.Button1, 0), tview.MouseLeftClick); (mouse == nil) != small {
				t.Fatal("mouse can reach hidden actions")
			}
		})
	}
}

func TestEgressHistoryPutsDestinationFirstAndOpensFullDetails(t *testing.T) {
	d, app, _ := testDesktop(t)
	snapshot := &workeregress.Snapshot{GeneralEgress: true, Generation: 1, PolicyID: "current"}
	destination := "long-application-registry.example.invalid:443"
	d.egressHistoryPage(sandbox.Worker{Name: "ワーカー-[red]"}, nil, snapshot, []policyevents.Event{
		{Destination: destination, Code: "domain_denied", PolicyID: snapshot.PolicyID},
	})
	_, page := d.pages.GetFrontPage()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(24, 10)
	page.SetRect(0, 0, 24, 10)
	page.Draw(screen)
	screen.Show()
	for _, want := range []string{"long-application", "domain_denied", "Refresh", "Back"} {
		if !strings.Contains(screenText(screen), want) {
			t.Fatal("narrow history hid", want)
		}
	}
	page.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { app.SetFocus(p) })
	name, details := d.pages.GetFrontPage()
	if name != "message" || !strings.Contains(details.(*tview.Flex).GetItem(0).(*tview.TextView).GetText(false), destination) {
		t.Fatal("full destination is unavailable", name)
	}
}
