# Dashboard

[Documentation index](README.md) · [Task workflow](tasks.md) · [Management API](management-api-contract.md)

Inspect tasks, edit plans and values, and manage local development resources in a browser.
The CLI remains the entry point for claims, execution contexts and validation results.

## Start and sign in

```sh
devtools dashboard
```

`devtools dashboard` and its alias `dashboard start` return JSON with server
metadata in `data.item` and a fresh login link in `data.item.url`. Open the full
link, including its fragment, to connect the browser tab. Choose a profile from
the header's dropdown; controls become available after authentication. Status
returns `data.item`; stop returns `data.changed`. See the [CLI output contract](cli-output.md).

Keep the login link private: its session can manage every profile for the current user.
The entry link is valid for five minutes and one use; the browser session
lasts eight idle hours or until the server stops.

## Browse tasks and dependencies

Use the **Profile** selector to choose a profile. The central list opens item
details; switch to **Graph** to explore dependencies. Details separate overview,
documents, and activity. Agents claim tasks and record progress and completion
through the CLI. The dashboard manages plans, definitions, and interventions.

## Edit plans and manage claims

**Dependencies** selects prerequisites by title. Workstream **Specification**
edits requirements and acceptance criteria as rows, with selectable requirement
references. **Plan** selects tasks and validations belonging to that workstream.
**Cancel** previews the open tasks that will also be canceled. Apply uses the
previewed profile revision; concurrent changes require a fresh review.

For a running task, **Revoke claim** records a reason and invalidates the observed
run's execution context. It leaves the task open. Running commands continue until
stopped separately; coordinate with the agent before assigning the work again.
The equivalent CLI command is:

```sh
devtools task unclaim TASK_ID --expected-run RUN_ID --if-revision REVISION --reason "Agent session ended" --request-id UUID
```

Use `task show TASK_ID` to obtain the current run and profile revision. After
revocation, a new CLI `task claim` starts a new execution. Late writes using the
previous context return `context_invalid`; the revocation reason remains in history.

## Navigation and refresh

Dashboard navigation is stored in the URL query: profile, workspace tab, environment,
search and filters, list/graph view, selected item and its detail tab, expanded process
details, and graph position. Reloading restores the current view. Browser Back and
Forward restore navigation; typing a filter updates the current history entry.
Values and secrets, edit forms, imported content, and raw logs stay in memory.
Authentication remains in the current browser session, so a view URL also requires
an authenticated session. If an environment was removed, the view returns to Common.

The visible dashboard checks for changes every five seconds and redraws when its
data changes. Polling pauses while the browser tab is hidden, a dialog or inline
editor is open, or an input has focus. Returning to the tab checks immediately.
Raw logs and validation evidence are loaded through their explicit controls.

## Manage values and vault sessions

**Variables & secrets** selects common values or an environment. Variables display
their values; secrets display storage metadata and accept a replacement value.
On builds with vault support, this view shows the shared key-session state and
explicit lock/unlock controls. Protected writes require an unlocked session; metadata
remains available while locked. Setup stays in the CLI. Unlock input is cleared after
sending, including uncertain responses; check status before re-entering it. Polling does
not unlock the vault or extend its idle timeout. See [local protection](secret-protection.md).

Editing an inherited value creates an override in the selected environment.
From **Common**, choose **Override in…** on a key, select an environment, and enter its value. Existing overrides are identified before saving. Saving opens that environment; the common value stays unchanged. Create an environment first if none exists. Secrets accept a new value with a masked input.

With an environment selected, **Inherited common**, **Overrides common**, and
**Env only** distinguish storage layers. Inherited rows use a muted background;
editing them offers **Create override**. **Remove override** restores inheritance,
while **Delete** removes an environment-only value. These labels apply equally to
variables and secrets and reflect stored layers, even when values are equal.
Use the Key, Kind, and Source column headers to search keys and filter by kind
and inheritance status together. Click a value to edit it in place,
then save or cancel; secret cells accept a replacement. **Import .env** accepts
a file or pasted assignments and previews key names and actions. New keys default
to secret; select public variables in the preview. **Overwrite existing values**
writes to the selected layer. Keeping existing values skips effective keys,
including inherited common values. The import is applied atomically.
Each import accepts up to 500 KB and 5,000 keys.
Removing an override restores the common value. Concurrent changes require a
refresh before editing again. See the [management API](management-api-contract.md)
for request and retry behavior.

## Servers, backups, and cleanup

The **Processes** screen manages [servers and captured logs](processes.md),
with explicit controls for [readiness](process-readiness.md) and local network resources.
**Storage & recovery** provides [backup and restore](backup.md) and
[cleanup previews and archives](cleanup.md). Review each target and preview before applying a change.

## Stop the dashboard

```sh
devtools dashboard status
devtools dashboard stop
```

Stopping the dashboard ends browser sessions. Task claims and managed development servers
have separate lifecycles and remain under their own controls.
