package ui

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pacificbelt30/codex-dock/internal/egresscontrol"
	"github.com/pacificbelt30/codex-dock/internal/networkpolicy"
	"github.com/pacificbelt30/codex-dock/internal/policyevents"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/pacificbelt30/codex-dock/internal/workeregress"
	"github.com/rivo/tview"
)

type desktopEgress struct {
	inspect func(context.Context, string) (workeregress.Snapshot, error)
	events  func(context.Context, string, func(policyevents.Event) error) error
	plan    func(context.Context, string, networkpolicy.Rules) (workeregress.ChangePlan, error)
	apply   func(context.Context, string, networkpolicy.Rules, string) (egresscontrol.State, error)
}

func nodeDesktopEgress(socket string) desktopEgress {
	return desktopEgress{
		inspect: func(ctx context.Context, worker string) (workeregress.Snapshot, error) {
			return workeregress.Inspect(ctx, socket, worker)
		},
		events: func(ctx context.Context, worker string, visit func(policyevents.Event) error) error {
			return workeregress.Events(ctx, socket, worker, false, visit)
		},
		plan: func(ctx context.Context, worker string, rules networkpolicy.Rules) (workeregress.ChangePlan, error) {
			return workeregress.PlanChange(ctx, socket, worker, rules)
		},
		apply: func(ctx context.Context, worker string, rules networkpolicy.Rules, planID string) (egresscontrol.State, error) {
			return workeregress.ApplyChange(ctx, socket, worker, rules, planID)
		},
	}
}

func (d *sandboxDesktop) openEgress(worker sandbox.Worker, names map[string]string) {
	d.egressHistoryPage(worker, names, nil, nil)
	d.readEgressHistory(worker, names)
}

func (d *sandboxDesktop) readEgressHistory(worker sandbox.Worker, names map[string]string) {
	d.read("Reading general egress policy and recent denials", func(ctx context.Context) (func(), error) {
		snapshot, err := d.backend.Egress.inspect(ctx, worker.ID)
		if err != nil {
			return nil, err
		}
		var events []policyevents.Event
		if snapshot.GeneralEgress {
			err = d.backend.Egress.events(ctx, worker.ID, func(event policyevents.Event) error {
				events = append(events, event)
				return nil
			})
		}
		return func() { d.egressHistoryPage(worker, names, &snapshot, events) }, err
	})
}

func (d *sandboxDesktop) egressHistoryPage(worker sandbox.Worker, names map[string]string, snapshot *workeregress.Snapshot, events []policyevents.Event) {
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" Egress denials · " + tview.Escape(worker.Name) + " ")
	switch {
	case snapshot == nil:
		list.AddItem("Refresh to inspect the selected proxy", "", 0, nil)
	case !snapshot.GeneralEgress:
		list.AddItem("This worker has no general egress proxy", "", 0, nil)
	case snapshot.UpdatePending:
		list.AddItem("Policy update pending", "New general connections are blocked. Use oaiso policy show to inspect the state.", 0, nil)
	case snapshot.Generation == 0:
		list.AddItem("Live policy changes unavailable", "This proxy has no live policy control.", 0, nil)
	default:
		list.AddItem(fmt.Sprintf("Current policy generation %d", snapshot.Generation), "", 0, nil)
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		destination := event.Destination
		if destination == "" {
			destination = "—"
		}
		reason := event.Code
		var selectEvent func()
		if egressGrantCandidate(snapshot, event) {
			reason = "Enter reviews a grant · not allowed"
			selectEvent = func() { d.reviewEgressGrant(worker, names, *snapshot, event) }
		} else {
			if snapshot != nil && event.PolicyID != snapshot.PolicyID {
				reason += " · policy changed"
			}
			selectEvent = func() {
				d.message(fmt.Sprintf("Destination: %s\nReason: %s\nRecorded: %s", destination, reason, event.Time.Local().Format("2006-01-02 15:04:05")))
			}
		}
		list.AddItem(tview.Escape(destination), tview.Escape(reason+" · "+event.Time.Local().Format("15:04:05")), 0, selectEvent)
	}
	if len(events) != 0 {
		list.SetCurrentItem(1)
	} else if snapshot != nil && snapshot.GeneralEgress {
		list.AddItem("No recent denials recorded", "", 0, nil)
	}

	form := tview.NewForm().AddButton("Refresh", func() { d.readEgressHistory(worker, names) }).AddButton("Back", d.back)
	navigate := d.pageFormNavigation(list, form, d.back)
	hint := tview.NewTextView().SetText("Enter details/review\nR refresh · Esc back")
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("Shared proxy history\nMay include other workers"), 2, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(form, 3, 0, false).
		AddItem(hint, 2, 0, false)
	fitDesktopActions(body, form, hint)
	body.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'r', 'R':
			d.readEgressHistory(worker, names)
			return nil
		case 'q', 'Q':
			d.back()
			return nil
		}
		return navigate(event)
	})
	d.pages.AddAndSwitchToPage("egress-history", body, true)
	d.app.SetFocus(list)
}

func egressGrantCandidate(snapshot *workeregress.Snapshot, event policyevents.Event) bool {
	if snapshot == nil || !snapshot.GeneralEgress || snapshot.Generation == 0 || snapshot.UpdatePending || snapshot.Quarantined || event.PolicyID != snapshot.PolicyID {
		return false
	}
	if event.Code != "default_denied" && event.Code != "domain_not_allowed" {
		return false
	}
	destination, err := networkpolicy.ParseDestination(event.Destination, "443")
	if err != nil {
		return false
	}
	_, err = netip.ParseAddr(destination.Host)
	return err != nil
}

func (d *sandboxDesktop) reviewEgressGrant(worker sandbox.Worker, names map[string]string, snapshot workeregress.Snapshot, event policyevents.Event) {
	d.read("Preparing the shared proxy change for review", func(ctx context.Context) (func(), error) {
		proposal, err := workeregress.Propose(ctx, snapshot, event.Destination)
		if err != nil {
			return nil, err
		}
		plan, err := d.backend.Egress.plan(ctx, worker.ID, proposal.Rules)
		if err != nil {
			return nil, err
		}
		if plan.Proxy != snapshot.Proxy || plan.Before.Generation != snapshot.Generation || !plan.Before.Ready {
			return nil, fmt.Errorf("proxy or policy changed; go back and refresh the denial history")
		}
		if err = proposal.ValidateBase(plan.Before); err != nil {
			return nil, err
		}
		return func() {
			d.reviewPage("egress-review", " Review egress policy change ", egressReviewText(proposal, plan, names), func() {
				d.applyEgressGrant(worker, names, proposal.Destination, plan)
			}, func() { d.pages.RemovePage("egress-review"); d.pages.SwitchToPage("egress-history") })
		}, nil
	})
}

func egressReviewText(proposal workeregress.Proposal, plan workeregress.ChangePlan, names map[string]string) string {
	var review strings.Builder
	fmt.Fprintln(&review, "Confirming closes all existing HTTP and CONNECT connections on this proxy.")
	fmt.Fprintf(&review, "Requested destination: %s\n", proposal.Destination)
	fmt.Fprintf(&review, "Add exact host: %s\n", proposal.Rules.AllowDomains[len(proposal.Rules.AllowDomains)-1])
	fmt.Fprintln(&review, "Affected workers at review time:")
	for _, id := range plan.Workers {
		if name := names[id]; name != "" && name != id {
			fmt.Fprintf(&review, "  %s (%s)\n", name, id)
		} else {
			fmt.Fprintf(&review, "  %s\n", id)
		}
	}
	fmt.Fprintf(&review, "\nShared proxy: %s\n", plan.Proxy)
	fmt.Fprintf(&review, "\nBefore (generation %d)\n%s\nAfter\n%s", plan.Before.Generation, egressRulesText(plan.Before.Rules), egressRulesText(plan.Proposed))
	fmt.Fprintln(&review, "\nPort rules apply to the whole proxy. DNS and private-address checks apply at connection time.")
	return review.String()
}

func egressRulesText(r networkpolicy.Rules) string {
	return fmt.Sprintf("Default deny: %t · Block private: %t\nAllow domains: %v\nDeny domains: %v\nAllow ports: %v\nDeny ports: %v\nAllow IP/CIDRs: %v\nDeny IP/CIDRs: %v\n",
		r.DefaultDeny, r.BlockPrivate, r.AllowDomains, r.DenyDomains, r.AllowPorts, r.DenyPorts, r.AllowCIDRs, r.DenyCIDRs)
}

func (d *sandboxDesktop) applyEgressGrant(worker sandbox.Worker, names map[string]string, destination string, plan workeregress.ChangePlan) {
	d.work(func(ctx context.Context) (func(), error) {
		state, err := d.backend.Egress.apply(ctx, worker.ID, plan.Proposed, plan.ID)
		return func() {
			d.egressHistoryPage(worker, names, nil, nil)
			d.pages.RemovePage("egress-review")
			if err != nil || !state.Ready || state.Generation <= plan.Before.Generation || state.PolicyID != workeregress.ID(plan.Proposed) {
				message := "Policy update not confirmed. Refresh the policy and review again before retrying."
				if err != nil {
					message += "\n\n" + err.Error()
				}
				d.message(message)
				return
			}
			d.message(fmt.Sprintf("Policy applied · generation %d → %d · ready\n\nExisting HTTP and CONNECT connections closed. Retry the original command for %s in its sandbox.", plan.Before.Generation, state.Generation, destination))
		}, nil
	})
}
