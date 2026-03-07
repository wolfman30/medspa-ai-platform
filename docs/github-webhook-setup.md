# GitHub Webhook Setup

Configure GitHub to send webhook events to the MedSpa AI Platform API.

## Steps

1. Go to your GitHub repository **Settings → Webhooks → Add webhook**

2. Configure:
   - **Payload URL:** `https://api-dev.aiwolfsolutions.com/webhooks/github`
   - **Content type:** `application/json`
   - **Secret:** Use the value of `GITHUB_WEBHOOK_SECRET` configured in the app
   - **SSL verification:** Enabled

3. Under **Which events would you like to trigger this webhook?**, select **Let me select individual events**, then check:
   - ✅ **Pull requests** — triggers PR review notifications on open/update/reopen
   - ✅ **Workflow runs** — triggers CI pass/fail notifications

4. Ensure **Active** is checked, then click **Add webhook**.

## What Happens

| Event | Actions Handled | Behavior |
|-------|----------------|----------|
| `pull_request` | `opened`, `synchronize`, `reopened` | Sends review notification with PR number, title, author, and link |
| `workflow_run` | `completed` | Sends CI success/failure notification |

## Backup

The QA PR Review cron job runs every 2 hours as a fallback. The webhook is the primary trigger.
