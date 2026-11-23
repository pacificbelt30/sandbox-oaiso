package ui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/rivo/tview"
)

type desktopJobs interface {
	StartJob(context.Context, string, sandbox.ExecOptions) (sandbox.Job, error)
	JobRetrySpec(context.Context, string, string) (sandbox.JobRetrySpec, error)
	ListJobs(context.Context, string) ([]sandbox.Job, error)
	JobLogs(context.Context, string, string, int64) (sandbox.JobOutput, error)
	KillJob(context.Context, string, string) error
}

func (d *sandboxDesktop) jobs(worker sandbox.Worker) {
	if d.backend.Jobs == nil {
		d.message("Background jobs are unavailable.")
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" Background jobs · " + tview.Escape(worker.Name) + " ")
	form := tview.NewForm().AddButton("Refresh", func() { d.jobs(worker) }).AddButton("New job…", func() { d.newJob(worker) }).AddButton("Back", d.back)
	hint := tview.NewTextView().SetText("Enter opens output\nTab actions · Esc back")
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(list, 0, 1, true).AddItem(form, 3, 0, false).AddItem(hint, 2, 0, false)
	fitDesktopActions(body, form, hint, "Refresh", "New job", "Back")
	body.SetInputCapture(d.pageFormNavigation(list, form, d.back))
	d.pages.AddAndSwitchToPage("jobs", body, true)
	d.app.SetFocus(list)
	d.read("Listing background jobs", func(ctx context.Context) (func(), error) {
		jobs, err := d.backend.Jobs.ListJobs(ctx, worker.ID)
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
		return func() {
			list.Clear()
			for _, job := range jobs {
				state := fmt.Sprintf("Exited · code %d", job.ExitCode)
				if job.Running {
					state = "Running · exit code pending"
				} else if job.ExitCode != 0 {
					state += " · Retry in output"
				}
				list.AddItem(tview.Escape(job.ID), state, 0, func() { d.jobOutput(worker, job) })
			}
			if len(jobs) == 0 {
				list.AddItem("No retained jobs", "Use New job to start a background command.", 0, nil)
			}
		}, err
	})
}

func (d *sandboxDesktop) newJob(worker sandbox.Worker) {
	if worker.Status != "running" {
		d.message("Start this sandbox with R before starting a job.")
		return
	}
	back := func() { d.jobs(worker) }
	var form *tview.Form
	form = commandForm(worker, func(command string) {
		d.confirmation(cleanJobText("Start background job in "+worker.Name+"?\nWorking directory: /workspace\nCommand:\n"+command+"\n\nClosing the TUI does not stop the job. Use the job output page to stop it."), func() {
			d.work(func(ctx context.Context) (func(), error) {
				job, err := d.backend.Jobs.StartJob(ctx, worker.ID, sandbox.ExecOptions{Argv: []string{"/bin/sh", "-lc", command}, WorkingDir: "/workspace"})
				return func() { d.jobOutput(worker, job) }, err
			})
		}, func() { d.pages.SwitchToPage("new-job"); d.app.SetFocus(form) })
	}, back)
	form.SetTitle(" New background job · Esc cancels ")
	d.pages.AddAndSwitchToPage("new-job", centered(form, 76, 9), true)
	d.app.SetFocus(form)
}

func cleanJobText(text string) string {
	var out strings.Builder
	for _, r := range strings.ToValidUTF8(strings.ReplaceAll(text, "\r\n", "\n"), "�") {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			out.WriteString(strconv.QuoteRuneToASCII(r))
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (d *sandboxDesktop) jobOutput(worker sandbox.Worker, job sandbox.Job) {
	d.showJobOutput(worker, job, nil, func() { d.jobs(worker) })
}

func (d *sandboxDesktop) reviewJobRetry(worker sandbox.Worker, job sandbox.Job, restore func()) {
	if worker.Status != "running" {
		d.message("Start this sandbox with R before retrying a job.")
		return
	}
	d.read("Reading the previous job's command", func(ctx context.Context) (func(), error) {
		spec, err := d.backend.Jobs.JobRetrySpec(ctx, worker.ID, job.ID)
		if err != nil {
			return nil, fmt.Errorf("retry needs a retained failed job with its working directory and no environment overrides.\n\nRefresh the jobs list if the record is missing. Use a matching TUI and node.\n\n%w", err)
		}
		args := make([]string, 0, len(spec.Argv))
		for _, a := range spec.Argv {
			args = append(args, strconv.Quote(a))
		}
		command := "Arguments (one per line):\n" + strings.Join(args, "\n")
		if len(spec.Argv) == 3 && slices.Equal(spec.Argv[:2], []string{"/bin/sh", "-lc"}) {
			command = "Command (/bin/sh -lc):\n" + spec.Argv[2]
		}
		text := cleanJobText(fmt.Sprintf("Sandbox: %s\nWorking directory: %s\nPrevious job: %s · exit code %d\n\n%s\n\nThe new job gets its own output and exit code.", worker.Name, spec.WorkingDir, spec.ID, spec.ExitCode, command))
		return func() {
			back := func() { d.pages.RemovePage("job-retry"); restore() }
			d.reviewPage("job-retry", " Review job retry ", text, func() {
				d.work(func(ctx context.Context) (func(), error) {
					retry, err := d.backend.Jobs.StartJob(ctx, spec.Worker, sandbox.ExecOptions{Argv: spec.Argv, WorkingDir: spec.WorkingDir})
					return func() {
						back()
						if err != nil {
							d.message("Retry start was not confirmed. Check the job list before trying again.\n\n" + err.Error())
							return
						}
						previous := sandbox.Job{ID: spec.ID, Worker: spec.Worker, ExitCode: spec.ExitCode}
						d.showJobOutput(worker, retry, &previous, func() { d.jobs(worker) })
					}, nil
				})
			}, back)
		}, nil
	})
}

func (d *sandboxDesktop) showJobOutput(worker sandbox.Worker, job sandbox.Job, previous *sandbox.Job, onBack func()) {
	if d.jobLogCancel != nil {
		d.jobLogCancel()
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.jobLogCancel = cancel
	back := func() { cancel(); d.pages.RemovePage("job-logs"); onBack() }
	view := tview.NewTextView().SetDynamicColors(false).SetScrollable(true)
	view.SetBorder(true).SetTitle(" Job output · " + tview.Escape(job.ID) + " ")
	state := tview.NewTextView().SetDynamicColors(false).SetText("Reading retained output…")
	restore := func() { d.pages.SwitchToPage("job-logs"); d.app.SetFocus(view) }
	form := tview.NewForm().AddButton("Stop job…", func() {
		d.confirmation(cleanJobText("Stop only job "+job.ID+" in "+worker.Name+"?\nThe VM and other jobs remain."), func() {
			d.work(func(c context.Context) (func(), error) {
				err := d.backend.Jobs.KillJob(c, worker.ID, job.ID)
				return func() { d.showJobOutput(worker, job, previous, onBack) }, err
			})
		}, restore)
	}).AddButton("Retry…", func() { d.reviewJobRetry(worker, job, restore) })
	retry := form.GetButton(1).SetDisabled(job.Running || job.ExitCode == 0 || worker.Status != "running")
	compact := []string{"Stop…", "Retry…"}
	if previous != nil {
		form.AddButton("Previous output", func() {
			d.showJobOutput(worker, *previous, nil, func() { d.showJobOutput(worker, job, previous, onBack) })
		})
		compact = append(compact, "Prev log")
	}
	form.AddButton("Back", back)
	follow := true
	hint := tview.NewTextView().SetText("Space pause/resume\nTab actions · Esc back")
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(state, 2, 0, false).AddItem(form, 3, 0, false).AddItem(hint, 2, 0, false)
	fitDesktopActions(body, form, hint, compact...)
	navigation := d.pageFormNavigation(view, form, back)
	body.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Rune() == ' ' && view.HasFocus() {
			follow = !follow
			return nil
		}
		return navigation(e)
	})
	d.pages.AddAndSwitchToPage("job-logs", body, true)
	d.app.SetFocus(view)
	go func() {
		var offset int64
		var text string
		truncated := false
		for {
			c, stop := context.WithTimeout(ctx, 10*time.Second)
			output, err := d.backend.Jobs.JobLogs(c, worker.ID, job.ID, offset)
			stop()
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				if output.Next < offset {
					err = fmt.Errorf("job log offset moved backwards")
				} else {
					offset = output.Next
					truncated = truncated || output.Truncated
					text += output.Output
					if len(text) > 256<<10 {
						text = text[len(text)-(256<<10):]
						for len(text) > 0 && !utf8.RuneStart(text[0]) {
							text = text[1:]
						}
					}
				}
			}
			rendered := cleanJobText(text)
			if truncated {
				rendered = "[Node output retention limit reached]\n" + rendered
			}
			d.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					state.SetText("Output unavailable: " + err.Error() + " · retrying")
					return
				}
				if output.Running {
					state.SetText("Running · exit code pending")
				} else {
					state.SetText("Exited · final status pending")
				}
				row, column := view.GetScrollOffset()
				view.SetText(rendered)
				if follow {
					view.ScrollToEnd()
				} else {
					view.ScrollTo(row, column)
				}
			})
			// Read the authoritative final exit code after draining retained output.
			if err == nil && !output.Running && len(output.Output) == 0 {
				c, stop := context.WithTimeout(ctx, 10*time.Second)
				jobs, e := d.backend.Jobs.ListJobs(c, worker.ID)
				stop()
				if ctx.Err() != nil {
					return
				}
				final := "Exited · final code unavailable"
				canRetry := false
				if e != nil {
					final += " · " + e.Error()
				} else {
					for _, j := range jobs {
						if j.ID == job.ID {
							if j.Running {
								final = "Running · exit status changed; reopen output"
							} else {
								final = fmt.Sprintf("Exited · code %d", j.ExitCode)
								canRetry = j.ExitCode != 0 && worker.Status == "running"
							}
							break
						}
					}
				}
				d.app.QueueUpdateDraw(func() {
					if ctx.Err() == nil {
						state.SetText(final)
						retry.SetDisabled(!canRetry)
					}
				})
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
}
