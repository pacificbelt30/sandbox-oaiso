# Review and apply live egress changes

[日本語](../../guides/live-policy.md) | **English**

- Available: **v0.0.4**, for egress proxies created by a build containing this feature.
- Run on the host with existing node-management permissions.
- **Updates affect the entire selected shared proxy. All its existing HTTP/CONNECT connections close.**

| Scope | Behavior |
|---|---|
| General HTTP/CONNECT | Confirm termination of old connections before allowing connections under the new rules |
| SSE / tunneled WebSocket | Closed along with their underlying HTTP/CONNECT connections |
| Fixed AI routes | Unchanged; their auth/model controls remain independent |
| Workers, jobs, previews | Continue running; applications relying on closed connections may fail |
| Private/metadata/DNS protections | Remain mandatory |
| Host configuration file | Not rewritten |
| Persistence | Proxy-private 0600 file, restored after process restart; removed with the proxy |

## Review a recorded denial in the TUI

Available: **v0.0.10 and later**. Use a worker with a general egress proxy and an account with node-management permissions on the host. Open the TUI from the project directory.

```bash
oaiso ui
```

1. Select a worker and press `E`, or choose **Egress denials and permissions** from **Actions F2**. The history belongs to the shared proxy. Press `R` to refresh it.
2. Select a row that says `Enter reviews a grant` and press `Enter`. Review the host to add, the before/after rules, the workers sharing the proxy, and the effect on existing HTTP/CONNECT connections. Press `Enter` on another denial to see its details.
3. Press `Tab` to reach **Confirm**, then press `Enter` to apply. Press `Esc` or choose **Cancel** to return to the history.
4. When the TUI shows `Policy applied`, a new generation, and `ready`, retry the original command in the worker. If application is unconfirmed, inspect the current rules with `oaiso policy show --worker <name>` before refreshing the history.

The proposal adds an exact hostname missing from the allowlist. The existing port rules apply to the entire proxy. Explicit denies, IP restrictions, and DNS checks stay in effect.

## 1. Draft a proposal

Example: propose an exact-host grant for `api.example.com:443`.

```bash
oaiso policy propose api.example.com:443 \
  --worker dev-worker --output ./policy-proposal.json
cat ./policy-proposal.json
```

Alternatively, use the latest retained denial:

```bash
oaiso policy propose --worker dev-worker --output ./policy-proposal.json
cat ./policy-proposal.json
```

- Choose one alternative. The output file must be new.
- Nothing is automatically approved or applied.
- The generated proposal adds an exact hostname. It never removes denies, broadens shared port grants or adds IP grants.
- Review/edit the complete `Rules` object manually for changes outside this narrow proposal operation.
- Denial history belongs to the shared proxy, may include other workers, and does not authenticate a requesting worker.
- DNS/address validation still happens at connection time; a proposal does not guarantee connectivity.

## 2. Review the change and its scope

```bash
oaiso policy plan --worker dev-worker --file ./policy-proposal.json
```

| Field | Review |
|---|---|
| `before` | Current rules, generation and readiness |
| `proposed` | Complete proposed rule set |
| `workers` | Workers currently sharing this proxy |
| `connections_closed` | The scope of connection termination |
| `id` | Change-plan ID |

- This is not a proof of policy widening/narrowing: overlapping domains, CIDRs and denies require review.
- Approval belongs to the caller with node permissions; the plan ID checks the proposed content, scope and generation.
- The worker list is an observation. The update is proxy-wide, including workers joining during application.

## 3. Apply the reviewed plan

Enter the plan ID from the previous output:

```bash
read -r -p 'Policy plan ID: ' policy_plan_id
```

```bash
oaiso policy apply --worker dev-worker \
  --file ./policy-proposal.json --plan-id "$policy_plan_id"
oaiso policy show --worker dev-worker --json
```

- Already-sent data and work started upstream cannot be retracted.
- Success means exit code 0, `ready: true`, and an increased generation.
- Stale plans/generations are refused. Review again before applying.
- Persistence/termination failures never report completion. A lost response may follow an actual change: inspect before retrying.
- `update_pending: true` blocks new general traffic. To finish the same intent, use the original proposal file to review a fresh plan and apply it again. Other stale proposals remain refused.
- Corrupt saved policy prevents startup instead of restoring older permissive launch arguments.

## Existing proxies and Go API

| Item | Behavior |
|---|---|
| Older proxy | Live updates refused; recover worker artifacts, then stop and recreate workers and proxy to enable control |
| New worker allocation | Refuse reuse when live rules differ from original launch rules; select a proxy for the desired policy |
| Management path | Existing node Exec → private 0600 Unix socket; no management TCP listener |
| Audit | Proxy-local `permission_change` intent/result records, without policy values or tokens |
| Propose | `client.ProposePolicy(ctx, workerID, destination)` |
| Plan | `client.PlanPolicyChange(ctx, workerID, cxd.EgressPolicy{...})` |
| Apply | `client.ApplyPolicyChange(ctx, workerID, policy, plan.ID)` |

- Editing configuration alone does not update a running proxy.
- Creation RequestID/options remain unchanged. Replaying creation does not reset a live policy change; inspect the actual policy instead.
- Emergency worker-wide stop remains `oaiso policy quarantine --worker dev-worker`; quarantine cannot be undone or restarted.
