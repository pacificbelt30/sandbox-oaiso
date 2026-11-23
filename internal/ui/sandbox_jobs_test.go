package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/rivo/tview"
)

type desktopJobFixture struct {
	mu      sync.Mutex
	calls   int
	killed  []string
	started []string
	retry   *sandbox.JobRetrySpec
	worker  string
	dir     string
}

func (f *desktopJobFixture) StartJob(_ context.Context, worker string, o sandbox.ExecOptions) (sandbox.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, strings.Join(o.Argv, " "))
	f.worker, f.dir = worker, o.WorkingDir
	return sandbox.Job{ID: "selected-job", Running: true}, nil
}
func (f *desktopJobFixture) ListJobs(context.Context, string) ([]sandbox.Job, error) {
	return []sandbox.Job{{ID: "selected-job", ExitCode: 37}, {ID: "other-job", Running: true}}, nil
}
func (f *desktopJobFixture) JobRetrySpec(context.Context, string, string) (sandbox.JobRetrySpec, error) {
	if f.retry != nil {
		return *f.retry, nil
	}
	return sandbox.JobRetrySpec{}, errors.New("no retained retry spec")
}
func (f *desktopJobFixture) JobLogs(context.Context, string, string, int64) (sandbox.JobOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return sandbox.JobOutput{Output: strings.Repeat("a", 300<<10) + "TAIL 日本語 [red]literal\x1b[31m", Next: 300<<10 + 40, Truncated: true}, nil
	}
	return sandbox.JobOutput{Next: 300<<10 + 40, Truncated: true}, nil
}
func (f *desktopJobFixture) KillJob(_ context.Context, worker, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, worker+":"+id)
	return nil
}

func TestJobLogTailRetainsFinalOutputAndDoesNotGuessExitCode(t *testing.T) {
	d, update, _ := runBrowser(t, &browserFiles{})
	fixture := &desktopJobFixture{}
	update(func() {
		d.backend.Jobs = fixture
		d.jobOutput(sandbox.Worker{ID: "worker", Name: "literal [red]", Status: "running"}, sandbox.Job{ID: "selected-job", Running: true})
	})
	t.Cleanup(func() {
		update(func() {
			if d.jobLogCancel != nil {
				d.jobLogCancel()
			}
		})
	})
	deadline := time.Now().Add(8 * time.Second)
	done := false
	for time.Now().Before(deadline) && !done {
		update(func() {
			_, p := d.pages.GetFrontPage()
			body := p.(*tview.Flex)
			status := body.GetItem(1).(*tview.TextView).GetText(false)
			if strings.Contains(status, "Exited · code 37") {
				text := body.GetItem(0).(*tview.TextView).GetText(false)
				if !strings.Contains(text, "TAIL 日本語 [red]literal") || !strings.Contains(text, "retention limit") || strings.ContainsRune(text, '\x1b') || len(text) > 270<<10 {
					t.Error("bounded final output lost or unsafe", len(text))
				}
				done = true
			}
		})
		if !done {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !done {
		t.Fatal("authoritative final status never displayed")
	}
	update(func() {
		_, p := d.pages.GetFrontPage()
		p.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { d.app.SetFocus(p) })
	})
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.killed) != 0 {
		t.Fatal("leaving logs stopped job", fixture.killed)
	}
}

func TestNewJobEscapeDoesNotStartCommand(t *testing.T) {
	d, update, wait := runBrowser(t, &browserFiles{})
	fixture := &desktopJobFixture{}
	update(func() {
		d.backend.Jobs = fixture
		d.newJob(sandbox.Worker{ID: "worker", Status: "running"})
		_, p := d.pages.GetFrontPage()
		p.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { d.app.SetFocus(p) })
	})
	wait()
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.started) != 0 || len(fixture.killed) != 0 {
		t.Fatal("cancel started or stopped jobs")
	}
}

func TestNewJobReviewDisplaysLiteralCommandMarkup(t *testing.T) {
	d, _, _ := testDesktop(t)
	fixture := &desktopJobFixture{}
	d.backend.Jobs = fixture
	d.newJob(sandbox.Worker{ID: "worker", Name: "literal [red]name", Status: "running"})
	var find func(tview.Primitive) *tview.Form
	find = func(p tview.Primitive) *tview.Form {
		if form, ok := p.(*tview.Form); ok {
			return form
		}
		if flex, ok := p.(*tview.Flex); ok {
			for i := 0; i < flex.GetItemCount(); i++ {
				if form := find(flex.GetItem(i)); form != nil {
					return form
				}
			}
		}
		return nil
	}
	_, p := d.pages.GetFrontPage()
	form := find(p)
	if form == nil {
		t.Fatal("command form missing")
	}
	form.GetFormItem(0).(*tview.InputField).SetText("printf '[red]literal'")
	form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(tview.Primitive) {})
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(120, 40)
	d.pages.SetRect(0, 0, 120, 40)
	d.pages.Draw(screen)
	screen.Show()
	if !strings.Contains(screenText(screen), "[red]literal") || len(fixture.started) != 0 {
		t.Fatal("review interpreted markup or started without approval")
	}
}

func TestRetryReviewRequiresApprovalAndRetainsExecutionSettings(t *testing.T) {
	d, update, wait := runBrowser(t, &browserFiles{})
	fixture := &desktopJobFixture{retry: &sandbox.JobRetrySpec{ID: "first", Worker: "worker", ExitCode: 23,
		Argv: []string{"/bin/sh", "-lc", "printf '[red]literal'"}, WorkingDir: "/workspace/subdir"}}
	worker := sandbox.Worker{ID: "worker", Status: "running"}
	open := func() { d.reviewJobRetry(worker, sandbox.Job{ID: "first"}, func() {}) }
	update(func() { d.backend.Jobs = fixture; open() })
	wait()
	update(func() {
		name, page := d.pages.GetFrontPage()
		if name != "job-retry" {
			t.Error("missing retry review", name)
			return
		}
		review := page.(*tview.Flex).GetItem(0).(*tview.TextView).GetText(false)
		for _, want := range []string{"exit code 23", "/workspace/subdir", "printf '[red]literal'"} {
			if !strings.Contains(review, want) {
				t.Errorf("retry review omitted %q", want)
			}
		}
		page.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { d.app.SetFocus(p) })
	})
	fixture.mu.Lock()
	if len(fixture.started) != 0 {
		t.Error("cancel started a new job", fixture.started)
	}
	fixture.mu.Unlock()
	update(open)
	wait()
	update(func() {
		name, page := d.pages.GetFrontPage()
		if name != "job-retry" {
			t.Error("missing retry review", name)
			return
		}
		confirm := page.(*tview.Flex).GetItem(1).(*tview.Form).GetButton(1)
		confirm.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(tview.Primitive) {})
	})
	wait()
	update(func() {
		name, page := d.pages.GetFrontPage()
		if name != "job-logs" || page.(*tview.Flex).GetItem(2).(*tview.Form).GetButtonIndex("Previous output") < 0 {
			t.Error("retry did not link to the original job", name)
		}
		if d.jobLogCancel != nil {
			d.jobLogCancel()
		}
	})
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.started) != 1 || fixture.started[0] != "/bin/sh -lc printf '[red]literal'" ||
		fixture.worker != "worker" || fixture.dir != "/workspace/subdir" {
		t.Fatal("retry changed the reviewed invocation", fixture.started, fixture.worker, fixture.dir)
	}
}
