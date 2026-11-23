package ui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/pacificbelt30/codex-dock/internal/egresscontrol"
	"github.com/pacificbelt30/codex-dock/internal/networkpolicy"
	"github.com/pacificbelt30/codex-dock/internal/policyevents"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/pacificbelt30/codex-dock/internal/sandboxux"
	"github.com/pacificbelt30/codex-dock/internal/workeregress"
	"github.com/rivo/tview"
)

func TestEgressReviewRequiresConfirmationAndUsesReviewedPlan(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(fmt.Sprintf("ready=%t", confirmed), func(t *testing.T) {
			d, update, wait := runBrowser(t, &browserFiles{listings: map[string][]sandboxux.FileEntry{".": {}}})
			rules := networkpolicy.Rules{DefaultDeny: true, BlockPrivate: true, AllowPorts: []int{443}, DenyDomains: []string{"blocked.example"}}
			snapshot := workeregress.Snapshot{Proxy: "shared-proxy", GeneralEgress: true, Generation: 12, Rules: rules, PolicyID: workeregress.ID(rules)}
			worker := sandbox.Worker{ID: "first", Name: "dev-worker"}
			names := map[string]string{worker.ID: worker.Name, "second": "test-worker"}
			event := policyevents.Event{PolicyID: snapshot.PolicyID, Destination: "registry.npmjs.org:443", Code: "default_denied"}
			var applied atomic.Int32
			update(func() {
				d.backend.Egress = desktopEgress{
					inspect: func(context.Context, string) (workeregress.Snapshot, error) { return snapshot, nil },
					events:  func(_ context.Context, _ string, visit func(policyevents.Event) error) error { return visit(event) },
					plan: func(_ context.Context, id string, next networkpolicy.Rules) (workeregress.ChangePlan, error) {
						if id != worker.ID || strings.Join(next.AllowDomains, ",") != "=registry.npmjs.org" {
							t.Errorf("unexpected proposal by %s: %+v", id, next)
						}
						return workeregress.ChangePlan{
							ID: "reviewed", Proxy: snapshot.Proxy, Proposed: next, Workers: []string{worker.ID, "second"},
							Before: egresscontrol.State{Generation: snapshot.Generation, Ready: true, PolicyID: snapshot.PolicyID, Rules: rules},
						}, nil
					},
					apply: func(_ context.Context, id string, next networkpolicy.Rules, planID string) (egresscontrol.State, error) {
						applied.Add(1)
						if id != worker.ID || planID != "reviewed" || strings.Join(next.AllowDomains, ",") != "=registry.npmjs.org" {
							t.Errorf("apply differs from review: %s %s %+v", id, planID, next)
						}
						return egresscontrol.State{Ready: confirmed, Generation: 13, PolicyID: workeregress.ID(next)}, nil
					},
				}
				d.openEgress(worker, names)
			})
			wait()
			key := func(key tcell.Key) {
				_, page := d.pages.GetFrontPage()
				page.InputHandler()(tcell.NewEventKey(key, 0, 0), func(p tview.Primitive) { d.app.SetFocus(p) })
			}
			update(func() { key(tcell.KeyEnter) })
			wait()
			update(func() {
				name, page := d.pages.GetFrontPage()
				if name != "egress-review" || applied.Load() != 0 {
					t.Error("selecting a denial skipped review", name)
					return
				}
				review := page.(*tview.Flex).GetItem(0).(*tview.TextView).GetText(false)
				for _, want := range []string{"=registry.npmjs.org", "test-worker", "all existing HTTP and CONNECT", "Before (generation 12)", "Allow ports: [443]", "Deny domains: [blocked.example]"} {
					if !strings.Contains(review, want) {
						t.Errorf("review omitted %q", want)
					}
				}
				key(tcell.KeyEscape)
				if name, _ := d.pages.GetFrontPage(); name != "egress-history" || applied.Load() != 0 {
					t.Error("cancel changed the policy or did not return to history", name)
				}
				key(tcell.KeyEnter)
			})
			wait()
			update(func() {
				if name, _ := d.pages.GetFrontPage(); name != "egress-review" {
					t.Error("could not review the same denial after cancel", name)
					return
				}
				key(tcell.KeyTab)
				key(tcell.KeyTab)
				key(tcell.KeyEnter)
			})
			wait()
			update(func() {
				name, modal := d.pages.GetFrontPage()
				if name != "message" || applied.Load() != 1 {
					t.Error("expected a single confirmed action followed by a result", name, applied.Load())
					return
				}
				screen := tcell.NewSimulationScreen("")
				if err := screen.Init(); err != nil {
					t.Error(err)
					return
				}
				defer screen.Fini()
				screen.SetSize(90, 26)
				modal.SetRect(0, 0, 90, 26)
				modal.Draw(screen)
				screen.Show()
				result := screenText(screen)
				if strings.Contains(result, "Policy applied") != confirmed || strings.Contains(result, "Policy update not confirmed") == confirmed {
					t.Error("incorrect policy result", result)
				}
				key(tcell.KeyEnter)
				if d.pages.HasPage("egress-review") {
					t.Error("old review remains available for reuse")
				}
			})
		})
	}
}

func TestEgressHistoryDoesNotSuggestGrantsForOtherDenials(t *testing.T) {
	d, _, _ := testDesktop(t)
	snapshot := workeregress.Snapshot{GeneralEgress: true, Generation: 1, PolicyID: "current"}
	d.egressHistoryPage(sandbox.Worker{Name: "worker"}, nil, &snapshot, []policyevents.Event{
		{Code: "domain_not_allowed", PolicyID: "current", Destination: "registry.npmjs.org:443"},
		{Code: "domain_not_allowed", PolicyID: "old", Destination: "other.example:443"},
		{Code: "default_denied", PolicyID: "current", Destination: "192.0.2.1:443"},
		{Code: "private_address", PolicyID: "current", Destination: "private.example:443"},
		{Code: "domain_denied", PolicyID: "current", Destination: "blocked.example:443"},
	})
	_, page := d.pages.GetFrontPage()
	list := page.(*tview.Flex).GetItem(1).(*tview.List)
	var candidates []string
	for i := range list.GetItemCount() {
		label, reason := list.GetItemText(i)
		if strings.Contains(reason, "Enter reviews") {
			candidates = append(candidates, label)
		}
	}
	if len(candidates) != 1 || !strings.Contains(candidates[0], "registry.npmjs.org:443") {
		t.Fatal("unexpected grant candidates", candidates)
	}
}
