# CI Build Notifications (Slack + Discord)

Drop-in build notifications for GitHub Actions, using the best actively-maintained action per platform. Both fire on **success and failure** (`!cancelled()` — superseded/cancelled builds stay quiet) and **only when the matching webhook secret is set**, so forks and unconfigured repos stay silent. Each message is status-aware (✅ success / ❌ failure) and `@here` is pinged only on failure (not on success).

Copy the three blocks below into any repo's workflow.

## Actions used (and why)

| Platform | Action                                                                                | Version  | Notes                                                                                                                                                                                                              |
| -------- | ------------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Slack    | [`slackapi/slack-github-action`](https://github.com/slackapi/slack-github-action)     | `v3.0.3` | **Official**, Slack-maintained. Replaces the now-archived `8398a7/action-slack`. Node 24 runtime. Pin to an exact tag (Slack's own recommendation).                                                                |
| Discord  | [`sarisia/actions-status-discord`](https://github.com/sarisia/actions-status-discord) | `v1`     | De-facto standard, actively maintained (latest `v1.16.0`, Node 24). The `v1` floating major tag tracks the latest patch. Auto-builds a rich embed (status, commit, author, ref, run link) from the GitHub context. |

Avoid the archived `8398a7/action-slack` and any action that hasn't shipped a release in the last year.

## Required secrets

| Secret                | How to create                                                                                                                              |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `SLACK_WEBHOOK_URL`   | Slack → **Incoming Webhooks** app → _Add New Webhook to Workspace_ → pick a channel → copy the `https://hooks.slack.com/services/...` URL. |
| `DISCORD_WEBHOOK_URL` | Discord → **Server Settings → Integrations → Webhooks → New Webhook** → pick a channel → _Copy Webhook URL_.                               |

Add each under **Repo → Settings → Secrets and variables → Actions**. Leave a secret unset to disable that platform — the step self-skips.

## 1. Expose webhook-presence flags (top-level `env:`)

GitHub forbids `secrets.*` inside some `if:` expressions, so surface booleans once:

```yaml
env:
  HAS_SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL != '' }}
  HAS_DISCORD_WEBHOOK_URL: ${{ secrets.DISCORD_WEBHOOK_URL != '' }}
```

## 2. Slack step (last step in the job)

The status-aware emoji uses GitHub's `A && B || C` expression idiom (operator `&&` binds tighter than `||`), so it resolves to ✅ for success and ❌ otherwise.

```yaml
- name: Notify Slack
  if: ${{ !cancelled() && env.HAS_SLACK_WEBHOOK_URL == 'true' }}
  uses: slackapi/slack-github-action@v3.0.3
  with:
    webhook: ${{ secrets.SLACK_WEBHOOK_URL }}
    webhook-type: incoming-webhook
    payload: |
      text: "${{ job.status == 'success' && ':white_check_mark:' || ':x:' }} ${{ github.repository }} build ${{ job.status }} on ${{ github.ref_name }}"
      blocks:
        - type: "section"
          text:
            type: "mrkdwn"
            text: "${{ job.status == 'success' && ':white_check_mark:' || ':x:' }} *${{ github.repository }}* build *${{ job.status }}* on `${{ github.ref_name }}`\n*Workflow:* ${{ github.workflow }}  •  *By:* ${{ github.actor }}  •  *Commit:* `${{ github.sha }}`"
        - type: "actions"
          elements:
            - type: "button"
              text:
                type: "plain_text"
                text: "View run"
              url: "${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}"
```

## 3. Discord step

```yaml
- name: Notify Discord
  if: ${{ !cancelled() && env.HAS_DISCORD_WEBHOOK_URL == 'true' }}
  uses: sarisia/actions-status-discord@v1
  with:
    webhook: ${{ secrets.DISCORD_WEBHOOK_URL }}
    status: ${{ job.status }} # sarisia colors the embed: green / grey / red
    title: ${{ github.repository }}
    content: "${{ job.status != 'success' && '@here' || '' }}" # ping only when not green
    username: GitHub Actions
    nofail: true # a webhook hiccup never fails the build
```

## Conventions baked in

- **Notify on success and failure, skip cancelled.** Both gate on `!cancelled()`, so success and failure post but superseded/concurrency-cancelled builds stay quiet. Want failure-only? Swap `!cancelled()` for `failure()`. Want literally every run including cancelled? Use `always()` (and add a `job.status == 'cancelled' && ':warning:'` branch to the Slack emoji).
- **Status-aware, ping only when needed.** Slack renders ✅ / ❌; Discord colors the embed and pings `@here` only when the status isn't `success`. Drop the `content:` line to never ping; set it to a constant `'@here'` to always ping.
- **Secret-gated.** The `HAS_*_WEBHOOK_URL` flag skips the step entirely when the secret is missing — safe in forks and new repos.
- **Injection-safe.** Only **trusted** GitHub context fields are interpolated (`repository`, `ref_name`, `workflow`, `actor`, `sha`, run URL). Never interpolate free-text attacker-controllable fields (commit message, PR title/body) into a `payload:` or `run:` — pass those via `env:` if ever needed. See <https://github.blog/security/vulnerability-research/how-to-catch-github-actions-workflow-injections-before-attackers-do/>.

## Updating versions

- Slack: pin to the newest exact tag — check <https://github.com/slackapi/slack-github-action/releases> and bump `@v3.0.3`.
- Discord: `@v1` auto-tracks the latest `v1.x`; only change it if a `v2` ships with a migration note.
