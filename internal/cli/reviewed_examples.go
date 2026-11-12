package cli

// Reviewed command-specific examples are shared by help and schema.examples.
// Uppercase tokens identify inspected inputs, not runnable synthetic identities.
type helpExample struct{ Purpose, Command string }

var reviewedExamples = map[string][]helpExample{
	"license": {
		{Purpose: "Included notices", Command: "devtools license"},
	},
	"init": {
		{Purpose: "Initialize this directory", Command: "devtools init"},
	},
	"help": {
		{Purpose: "Group overview", Command: "devtools help proxy"},
		{Purpose: "One command", Command: "devtools help proxy list"},
	},
	"version": {
		{Purpose: "Build and protocol", Command: "devtools version"},
	},
	"schema": {
		{Purpose: "One input/output contract", Command: "devtools schema proxy list"},
		{Purpose: "Complete catalog", Command: "devtools schema --all"},
	},
	"completion": {
		{Purpose: "Zsh script", Command: "devtools completion zsh"},
		{Purpose: "Bash script", Command: "devtools completion bash"},
	},
	"update": {
		{Purpose: "Latest stable", Command: "devtools update"},
		{Purpose: "Selected release", Command: "devtools update --version VERSION"},
	},
	"config schema": {
		{Purpose: "TOML and edit schemas", Command: "devtools config schema"},
	},
	"config inspect": {
		{Purpose: "Current project and revision", Command: "devtools config inspect"},
		{Purpose: "Another project", Command: "devtools config inspect --dir ./app"},
	},
	"config check": {
		{Purpose: "Static declarations", Command: "devtools config check"},
		{Purpose: "Another project", Command: "devtools config check --dir ./app"},
	},
	"config fmt": {
		{Purpose: "Preview formatting", Command: "devtools config fmt --dry-run"},
		{Purpose: "Fail if unformatted", Command: "devtools config fmt --check"},
		{Purpose: "Apply inspected formatting", Command: "devtools config fmt --revision REVISION"},
	},
	"config edit": {
		{Purpose: "Preview typed operations", Command: "devtools config edit --revision REVISION --file ./config-edit.json --dry-run"},
		{Purpose: "Apply reviewed operations", Command: "devtools config edit --revision REVISION --file ./config-edit.json"},
	},
	"config migrate": {
		{Purpose: "Preview migration", Command: "devtools config migrate"},
		{Purpose: "Apply approved migration", Command: "devtools config migrate --revision REVISION --apply DIGEST"},
	},
	"config tool add": {
		{Purpose: "Preview a Node requirement", Command: "devtools config tool add node --file ./tool.json --dry-run"},
		{Purpose: "Add the reviewed tool", Command: "devtools config tool add node --file ./tool.json"},
	},
	"config tool update": {
		{Purpose: "Preview requirement patch", Command: "devtools config tool update node --revision REVISION --file ./tool-patch.json --dry-run"},
		{Purpose: "Apply the patch", Command: "devtools config tool update node --revision REVISION --file ./tool-patch.json"},
	},
	"config tool remove": {
		{Purpose: "Check affected references", Command: "devtools config tool remove node --revision REVISION --dry-run"},
		{Purpose: "Remove after review", Command: "devtools config tool remove node --revision REVISION"},
	},
	"config preset add": {
		{Purpose: "Preview shared requirements", Command: "devtools config preset add frontend --file ./preset.json --dry-run"},
		{Purpose: "Add the preset", Command: "devtools config preset add frontend --file ./preset.json"},
	},
	"config preset update": {
		{Purpose: "Preview membership patch", Command: "devtools config preset update frontend --revision REVISION --file ./preset-patch.json --dry-run"},
		{Purpose: "Apply the patch", Command: "devtools config preset update frontend --revision REVISION --file ./preset-patch.json"},
	},
	"config preset remove": {
		{Purpose: "Inspect remaining references", Command: "devtools config preset remove frontend --revision REVISION --dry-run"},
		{Purpose: "Remove the preset", Command: "devtools config preset remove frontend --revision REVISION"},
	},
	"command env": {
		{Purpose: "Public network bindings", Command: "devtools command env dev:web"},
		{Purpose: "Another project", Command: "devtools command env dev:web --dir ./app"},
	},
	"command list": {
		{Purpose: "Discover runnable commands", Command: "devtools command list"},
		{Purpose: "Another project", Command: "devtools command list --dir ./app"},
	},
	"command inspect": {
		{Purpose: "Effective command definition", Command: "devtools command inspect dev:web"},
		{Purpose: "Another project", Command: "devtools command inspect dev:web --dir ./app"},
	},
	"command run": {
		{Purpose: "Configured command", Command: "devtools run dev:web"},
		{Purpose: "Common values layer", Command: "devtools run --env '' dev:web"},
		{Purpose: "Direct program", Command: "devtools run -- node --version"},
	},
	"command add": {
		{Purpose: "Preview a command", Command: "devtools command add dev:web --dry-run -- node scripts/dev.mjs"},
		{Purpose: "Add the reviewed command", Command: "devtools command add dev:web -- node scripts/dev.mjs"},
	},
	"command update": {
		{Purpose: "Preview definition patch", Command: "devtools command update dev:web --revision REVISION --file ./command-patch.json --dry-run"},
		{Purpose: "Apply patch", Command: "devtools command update dev:web --revision REVISION --file ./command-patch.json"},
	},
	"command remove": {
		{Purpose: "Inspect affected references", Command: "devtools command remove dev:web --revision REVISION --dry-run"},
		{Purpose: "Remove reviewed command", Command: "devtools command remove dev:web --revision REVISION"},
	},
	"command rename": {
		{Purpose: "Preview distinct names", Command: "devtools command rename dev:web dev:storefront --revision REVISION --dry-run"},
		{Purpose: "Apply rename", Command: "devtools command rename dev:web dev:storefront --revision REVISION"},
	},
	"command explain": {
		{Purpose: "Declaration provenance", Command: "devtools command explain dev:web"},
		{Purpose: "Explain an override", Command: "devtools command explain dev:web --env staging --bundle frontend"},
	},
	"project inspect": {
		{Purpose: "Resolve current project", Command: "devtools project inspect"},
		{Purpose: "Resolve another directory", Command: "devtools project inspect --dir ./app"},
		{Purpose: "Explicit profile without config lookup", Command: "devtools project inspect --profile my-app"},
	},
	"project request show": {
		{Purpose: "Recover original operation result", Command: "devtools project request show PREVIOUS_REQUEST_UUID"},
	},
	"project up": {
		{Purpose: "Start one command", Command: "devtools project up dev:web"},
		{Purpose: "Start a group with retained logs", Command: "devtools project up dev:web dev:api --capture-logs"},
	},
	"project status": {
		{Purpose: "All active project commands", Command: "devtools project status"},
		{Purpose: "Selected command", Command: "devtools project status dev:web"},
	},
	"project restart": {
		{Purpose: "Restart one command", Command: "devtools project restart dev:web"},
		{Purpose: "Restart selected group", Command: "devtools project restart dev:web dev:api --timeout 60s"},
	},
	"project logs": {
		{Purpose: "Retained command output", Command: "devtools project logs dev:web"},
		{Purpose: "Another project", Command: "devtools project logs dev:web --dir ./app"},
	},
	"project down": {
		{Purpose: "Stop selected command", Command: "devtools project down dev:web"},
		{Purpose: "Stop all active project commands", Command: "devtools project down"},
	},
	"process start": {
		{Purpose: "Managed command", Command: "devtools process start dev:web"},
		{Purpose: "Retain redacted logs", Command: "devtools process start dev:web --capture-logs"},
	},
	"process list": {
		{Purpose: "Current project services", Command: "devtools process list"},
		{Purpose: "One profile", Command: "devtools process list --profile my-app"},
		{Purpose: "Execution history with a bounded page", Command: "devtools process list --profile my-app --view history --limit 50"},
		{Purpose: "Current services across all profiles", Command: "devtools process list --all-profiles"},
	},
	"process status": {
		{Purpose: "Current execution state", Command: "devtools process status EXECUTION_ID"},
	},
	"process stop": {
		{Purpose: "Stop the inspected execution", Command: "devtools process stop EXECUTION_ID"},
	},
	"process restart": {
		{Purpose: "Inherit prior overrides", Command: "devtools process restart EXECUTION_ID"},
		{Purpose: "Choose another bundle", Command: "devtools process restart EXECUTION_ID --bundle frontend"},
	},
	"process logs": {
		{Purpose: "Retained execution output", Command: "devtools process logs EXECUTION_ID"},
	},
	"process check": {
		{Purpose: "One readiness probe", Command: "devtools process check EXECUTION_ID"},
	},
	"process wait": {
		{Purpose: "Wait for readiness", Command: "devtools process wait EXECUTION_ID"},
		{Purpose: "Explicit wait budget", Command: "devtools process wait EXECUTION_ID --timeout 60s"},
	},
	"process request show": {
		{Purpose: "Recover original process operation", Command: "devtools process request show PREVIOUS_REQUEST_UUID"},
	},
	"doctor": {
		{Purpose: "All configured commands", Command: "devtools doctor"},
		{Purpose: "One foreground command", Command: "devtools doctor dev:web"},
		{Purpose: "Managed prerequisites with failure exit", Command: "devtools doctor dev:web --mode managed --check"},
	},
	"diagnostics": {
		{Purpose: "Local subsystem summary", Command: "devtools diagnostics"},
	},
	"vault status": {
		{Purpose: "Protection and session state", Command: "devtools vault status"},
	},
	"vault init": {
		{Purpose: "Native protection", Command: "devtools vault init --backend native --interactive"},
		{Purpose: "Private passphrase file", Command: "devtools vault init --backend passphrase --passphrase-file ./vault-passphrase"},
	},
	"vault recover": {
		{Purpose: "Finish pending storage recovery", Command: "devtools vault recover"},
	},
	"vault rewrap": {
		{Purpose: "Change passphrase protection", Command: "devtools vault rewrap --from-backend passphrase --backend passphrase --current-passphrase-file ./old-passphrase --new-passphrase-file ./new-passphrase"},
		{Purpose: "Switch to native protection", Command: "devtools vault rewrap --from-backend passphrase --backend native --current-passphrase-file ./old-passphrase --interactive"},
	},
	"vault unlock": {
		{Purpose: "Native OS authentication prompt", Command: "devtools vault unlock --interactive"},
		{Purpose: "Passphrase terminal input", Command: "devtools vault unlock"},
		{Purpose: "Private file input", Command: "devtools vault unlock --passphrase-file ./vault-passphrase"},
	},
	"vault lock": {
		{Purpose: "Lock reusable key session", Command: "devtools vault lock"},
	},
	"variable set": {
		{Purpose: "Public literal", Command: "devtools variable set API_URL --value https://api.example.test"},
		{Purpose: "Named environment", Command: "devtools variable set API_URL --env staging --file ./api-url.txt"},
	},
	"variable list": {
		{Purpose: "Common layer metadata", Command: "devtools variable list"},
		{Purpose: "Named environment metadata", Command: "devtools variable list --profile my-app --env staging"},
	},
	"variable unset": {
		{Purpose: "Remove common-layer override", Command: "devtools variable unset API_URL"},
		{Purpose: "Guarded named-layer removal", Command: "devtools variable unset API_URL --env staging --revision REVISION"},
	},
	"variable get": {
		{Purpose: "Read public value", Command: "devtools variable get API_URL"},
		{Purpose: "Read selected environment", Command: "devtools variable get API_URL --env staging"},
	},
	"secret set": {
		{Purpose: "Read private file", Command: "devtools secret set API_TOKEN --file ./api-token"},
		{Purpose: "Read piped stdin", Command: "devtools secret set API_TOKEN --stdin"},
	},
	"secret list": {
		{Purpose: "Common-layer metadata", Command: "devtools secret list"},
		{Purpose: "Named-layer metadata", Command: "devtools secret list --profile my-app --env staging"},
	},
	"secret unset": {
		{Purpose: "Remove exact common-layer secret", Command: "devtools secret unset API_TOKEN"},
		{Purpose: "Guarded environment removal", Command: "devtools secret unset API_TOKEN --env staging --revision REVISION"},
	},
	"secret generate": {
		{Purpose: "Create exact-layer random secret", Command: "devtools secret generate SESSION_KEY --revision REVISION"},
		{Purpose: "Explicitly replace existing exact-layer secret", Command: "devtools secret generate SESSION_KEY --replace --revision REVISION"},
	},
	"env create": {
		{Purpose: "Add named environment", Command: "devtools env create staging"},
		{Purpose: "Guarded creation in one profile", Command: "devtools env create staging --profile my-app --revision REVISION"},
	},
	"env list": {
		{Purpose: "Current profile", Command: "devtools env list"},
		{Purpose: "Explicit profile", Command: "devtools env list --profile my-app"},
	},
	"env remove": {
		{Purpose: "Remove named environment", Command: "devtools env remove staging"},
		{Purpose: "Guarded removal", Command: "devtools env remove staging --revision REVISION"},
	},
	"bundle list": {
		{Purpose: "Shared bundles and edit revision", Command: "devtools bundle list"},
		{Purpose: "One profile", Command: "devtools bundle list --profile my-app"},
	},
	"bundle set": {
		{Purpose: "Preview chosen keys", Command: "devtools bundle set frontend --key API_URL --key API_TOKEN --dry-run --revision REVISION"},
		{Purpose: "Set reviewed membership", Command: "devtools bundle set frontend --key API_URL --key API_TOKEN --revision REVISION"},
		{Purpose: "Explicit empty membership", Command: "devtools bundle set frontend --revision REVISION"},
	},
	"bundle rename": {
		{Purpose: "Preview distinct names", Command: "devtools bundle rename frontend storefront --dry-run --revision REVISION"},
		{Purpose: "Apply reviewed rename", Command: "devtools bundle rename frontend storefront --revision REVISION"},
	},
	"bundle remove": {
		{Purpose: "Preview removal", Command: "devtools bundle remove frontend --dry-run --revision REVISION"},
		{Purpose: "Remove bundle, retaining values", Command: "devtools bundle remove frontend --revision REVISION"},
	},
	"values revision": {
		{Purpose: "Current edit token", Command: "devtools values revision"},
		{Purpose: "Explicit profile and layer", Command: "devtools values revision --profile my-app --env staging"},
	},
	"values request show": {
		{Purpose: "Original guarded edit receipt", Command: "devtools values request show PREVIOUS_REQUEST_UUID --profile my-app"},
	},
	"values inspect": {
		{Purpose: "All selected inputs", Command: "devtools values inspect"},
		{Purpose: "Named bundle and environment", Command: "devtools values inspect --profile my-app --env staging --bundle frontend"},
	},
	"values uses": {
		{Purpose: "Declared and delivered uses", Command: "devtools values uses"},
		{Purpose: "Another project", Command: "devtools values uses --dir ./app"},
	},
	"values execution prepare": {
		{Purpose: "Freeze explicit scoped selection", Command: "devtools values execution prepare --profile my-app --file ./prepare.json"},
	},
	"values execution approve": {
		{Purpose: "Approve current frozen revision", Command: "devtools values execution approve INPUT_EXECUTION_ID --profile my-app --file ./approve.json"},
	},
	"values execution cancel": {
		{Purpose: "Cancel pending frozen input", Command: "devtools values execution cancel INPUT_EXECUTION_ID --profile my-app --file ./cancel.json"},
	},
	"values execution deliver": {
		{Purpose: "Send approved input once", Command: "devtools values execution deliver INPUT_EXECUTION_ID --profile my-app --file ./deliver.json"},
	},
	"values execution report": {
		{Purpose: "Record actual runner facts", Command: "devtools values execution report INPUT_EXECUTION_ID --profile my-app --file ./report.json"},
	},
	"values execution status": {
		{Purpose: "Inspect execution identity", Command: "devtools values execution status INPUT_EXECUTION_ID --profile my-app"},
		{Purpose: "Recover by original prepare UUID", Command: "devtools values execution status --profile my-app --request-id PREVIOUS_REQUEST_UUID"},
	},
	"values execution list": {
		{Purpose: "First page", Command: "devtools values execution list --profile my-app --limit 20"},
		{Purpose: "Same query continuation", Command: "devtools values execution list --profile my-app --limit 20 --cursor CURSOR"},
	},
	"values runner address": {
		{Purpose: "Endpoint for actual runner instance", Command: "devtools values runner address RUNNER_ID --instance RUNNER_INSTANCE_ID"},
	},
	"values runner register": {
		{Purpose: "Probe and register live instance", Command: "devtools values runner register --file ./runner.json"},
	},
	"values runner unregister": {
		{Purpose: "Disable observed instance", Command: "devtools values runner unregister RUNNER_ID --instance RUNNER_INSTANCE_ID"},
	},
	"values runner show": {
		{Purpose: "Passive registration metadata", Command: "devtools values runner show RUNNER_ID"},
	},
	"port list": {
		{Purpose: "Current profile locations", Command: "devtools port list"},
		{Purpose: "One profile", Command: "devtools port list --profile my-app"},
		{Purpose: "One location", Command: "devtools port list --dir ./app"},
	},
	"port show": {
		{Purpose: "Current location assignment", Command: "devtools port show web"},
		{Purpose: "Named location", Command: "devtools port show web --profile my-app --instance worktree-a"},
	},
	"port allocate": {
		{Purpose: "Allocate or reuse current assignment", Command: "devtools port allocate web"},
		{Purpose: "Named location", Command: "devtools port allocate web --profile my-app --instance worktree-a"},
	},
	"port check": {
		{Purpose: "Observe current assignment", Command: "devtools port check web"},
		{Purpose: "Named location", Command: "devtools port check web --profile my-app --instance worktree-a"},
	},
	"port release": {
		{Purpose: "Release inspected unused assignment", Command: "devtools port release web"},
		{Purpose: "Named location", Command: "devtools port release web --profile my-app --instance worktree-a"},
	},
	"port prune": {
		{Purpose: "Prune missing locations in current profile", Command: "devtools port prune"},
		{Purpose: "Explicit profile", Command: "devtools port prune --profile my-app"},
	},
	"instance list": {
		{Purpose: "Current profile locations", Command: "devtools instance list"},
		{Purpose: "Explicit profile", Command: "devtools instance list --profile my-app"},
	},
	"instance show": {
		{Purpose: "Current location", Command: "devtools instance show"},
		{Purpose: "Named location", Command: "devtools instance show --profile my-app --instance worktree-a"},
	},
	"instance name": {
		{Purpose: "Name current location", Command: "devtools instance name worktree-a"},
		{Purpose: "Name another directory", Command: "devtools instance name worktree-a --dir ./app"},
	},
	"instance move": {
		{Purpose: "Move observed location record", Command: "devtools instance move worktree-a --profile my-app --dir ./app-moved"},
	},
	"instance remove": {
		{Purpose: "Remove inspected unused location", Command: "devtools instance remove worktree-a --profile my-app"},
	},
	"proxy env": {
		{Purpose: "Project route bindings", Command: "devtools proxy env api"},
		{Purpose: "Global route bindings", Command: "devtools proxy env api --global"},
	},
	"proxy start": {
		{Purpose: "Default listener", Command: "devtools proxy start"},
		{Purpose: "Named listener", Command: "devtools proxy start --listener local"},
		{Purpose: "All configured listeners", Command: "devtools proxy start --all"},
	},
	"proxy status": {
		{Purpose: "Default listener", Command: "devtools proxy status"},
		{Purpose: "Named listener", Command: "devtools proxy status --listener local"},
		{Purpose: "All configured listeners", Command: "devtools proxy status --all"},
	},
	"proxy list": {
		{Purpose: "Current project routes", Command: "devtools proxy list"},
		{Purpose: "All profiles and global routes", Command: "devtools proxy list --all-profiles"},
		{Purpose: "One profile across its worktrees", Command: "devtools proxy list --profile my-app"},
		{Purpose: "Global routes only", Command: "devtools proxy list --global"},
	},
	"proxy stop": {
		{Purpose: "Default listener", Command: "devtools proxy stop"},
		{Purpose: "Named listener", Command: "devtools proxy stop --listener local"},
		{Purpose: "All configured listeners", Command: "devtools proxy stop --all"},
	},
	"proxy setup": {
		{Purpose: "Read-only setup check", Command: "devtools proxy setup --check"},
		{Purpose: "Explicit low-port setup", Command: "devtools proxy setup"},
		{Purpose: "Remove setup intent", Command: "devtools proxy setup --remove"},
	},
	"proxy teardown": {
		{Purpose: "Preview selected listener", Command: "devtools proxy teardown --listener local"},
		{Purpose: "Apply that preview", Command: "devtools proxy teardown --listener local --apply DIGEST"},
		{Purpose: "Preview all listeners and shared CA removal", Command: "devtools proxy teardown --all --remove-ca"},
	},
	"proxy configure": {
		{Purpose: "Configure stopped HTTP listener", Command: "devtools proxy configure --listener local --http-address 127.0.0.1:8080"},
		{Purpose: "Enable HTTPS explicitly", Command: "devtools proxy configure --listener local --https-address 127.0.0.1:8443"},
	},
	"proxy restart": {
		{Purpose: "Named listener", Command: "devtools proxy restart --listener local"},
		{Purpose: "All configured listeners", Command: "devtools proxy restart --all"},
	},
	"proxy logs": {
		{Purpose: "Default listener events", Command: "devtools proxy logs"},
		{Purpose: "All listener events", Command: "devtools proxy logs --all"},
	},
	"proxy hosts preview": {
		{Purpose: "Project route mapping", Command: "devtools proxy hosts preview api"},
		{Purpose: "Global route mapping", Command: "devtools proxy hosts preview api --global"},
	},
	"proxy hosts apply": {
		{Purpose: "Apply reviewed project mapping", Command: "devtools proxy hosts apply api --apply DIGEST"},
		{Purpose: "Apply reviewed global mapping", Command: "devtools proxy hosts apply api --global --apply DIGEST"},
	},
	"proxy hosts remove": {
		{Purpose: "Remove owned project mapping", Command: "devtools proxy hosts remove api"},
		{Purpose: "Remove owned global mapping", Command: "devtools proxy hosts remove api --global"},
	},
	"proxy run": {
		{Purpose: "Foreground global backend", Command: "devtools proxy run --global api -- node scripts/server.mjs"},
	},
	"proxy add": {
		{Purpose: "Declare global backend", Command: "devtools proxy add api 3000 --global --host api.example.test"},
		{Purpose: "Select listener and DNS policy", Command: "devtools proxy add api 3000 --global --host api.example.test --listener local --dns external"},
	},
	"proxy rm": {
		{Purpose: "Remove one global route", Command: "devtools proxy rm api --global"},
	},
	"proxy clear": {
		{Purpose: "Explicitly remove all global routes", Command: "devtools proxy clear --global --all"},
	},
	"proxy up": {
		{Purpose: "Activate project route", Command: "devtools proxy up api"},
		{Purpose: "Activate global route", Command: "devtools proxy up api --global"},
		{Purpose: "Activate all project routes", Command: "devtools proxy up --all"},
	},
	"proxy down": {
		{Purpose: "Deactivate project route", Command: "devtools proxy down api"},
		{Purpose: "Deactivate global route", Command: "devtools proxy down api --global"},
		{Purpose: "Deactivate all project routes", Command: "devtools proxy down --all"},
	},
	"proxy ca init": {
		{Purpose: "Create or reuse local CA", Command: "devtools proxy ca init"},
	},
	"proxy ca status": {
		{Purpose: "Local CA and OS trust", Command: "devtools proxy ca status"},
	},
	"proxy ca export": {
		{Purpose: "Public certificate in result", Command: "devtools proxy ca export"},
		{Purpose: "New public certificate file", Command: "devtools proxy ca export --output ./devtools-ca.pem"},
	},
	"proxy trust": {
		{Purpose: "Explicitly trust this CA in the host OS", Command: "devtools proxy trust"},
	},
	"proxy untrust": {
		{Purpose: "Remove host OS trust", Command: "devtools proxy untrust"},
	},
	"expose start": {
		{Purpose: "Existing local route URL", Command: "devtools expose start api"},
		{Purpose: "Authenticated external sharing", Command: "devtools expose start api --provider cloudflared --auth-file ./expose-auth"},
	},
	"expose list": {
		{Purpose: "Existing exposure identities", Command: "devtools expose list"},
	},
	"expose status": {
		{Purpose: "Current exposure state", Command: "devtools expose status EXPOSURE_ID"},
	},
	"expose stop": {
		{Purpose: "Stop inspected external exposure", Command: "devtools expose stop EXPOSURE_ID"},
	},
	"backup keygen": {
		{Purpose: "Create private/public key files", Command: "devtools backup keygen --identity-file ./backup.identity --recipient-file ./backup.recipient"},
	},
	"backup configure": {
		{Purpose: "Use public backup recipient", Command: "devtools backup configure --directory ./backups --recipient-file ./backup.recipient"},
	},
	"backup status": {
		{Purpose: "Backup destination", Command: "devtools backup status"},
	},
	"backup create": {
		{Purpose: "All profiles with a backup passphrase or configured hybrid recipient", Command: "devtools backup create"},
		{Purpose: "One profile at a new path", Command: "devtools backup create --profile my-app --output ./my-app.age --recipient-file ./backup.recipient"},
	},
	"backup inspect": {
		{Purpose: "Authenticated archive metadata", Command: "devtools backup inspect --file ./my-app.age --identity-file ./backup.identity"},
	},
	"backup restore": {
		{Purpose: "Preview restoration into a new profile", Command: "devtools backup restore --file ./my-app.age --identity-file ./backup.identity --profile my-app --as restored-app"},
		{Purpose: "Apply that preview", Command: "devtools backup restore --file ./my-app.age --identity-file ./backup.identity --profile my-app --as restored-app --apply DIGEST"},
	},
	"cleanup preview": {
		{Purpose: "Recoverable cleanup candidates", Command: "devtools cleanup preview"},
		{Purpose: "One profile", Command: "devtools cleanup preview --profile my-app"},
	},
	"cleanup apply": {
		{Purpose: "Archive one selected candidate", Command: "devtools cleanup apply PLAN_ID --item CANDIDATE_ID"},
	},
	"cleanup archives": {
		{Purpose: "Recoverable archives", Command: "devtools cleanup archives"},
	},
	"cleanup maintain": {
		{Purpose: "Inspect the next bounded retention pass", Command: "devtools cleanup maintain --dry-run --limit 32"},
		{Purpose: "Run one bounded retention pass", Command: "devtools cleanup maintain --limit 32"},
	},
	"cleanup restore": {
		{Purpose: "Restore inspected archive", Command: "devtools cleanup restore ARCHIVE_ID"},
	},
	"cleanup purge": {
		{Purpose: "Permanently remove eligible archive payload", Command: "devtools cleanup purge ARCHIVE_ID"},
	},
	"profile list": {
		{Purpose: "Known profile metadata", Command: "devtools profile list"},
	},
	"profile inspect": {
		{Purpose: "One profile's resources", Command: "devtools profile inspect my-app"},
	},
	"profile diff": {
		{Purpose: "Compare different profiles", Command: "devtools profile diff my-app staging-app"},
	},
	"profile rename": {
		{Purpose: "Rename a local profile", Command: "devtools profile rename my-app renamed-app"},
	},
	"profile transfer prepare": {
		{Purpose: "Prepare recipient on destination device", Command: "devtools profile transfer prepare"},
	},
	"profile export": {
		{Purpose: "Passphrase archive with hidden prompt", Command: "devtools profile export --profile my-app --output ./my-app.age"},
		{Purpose: "Recipient archive for destination device", Command: "devtools profile export --profile my-app --output ./my-app.age --recipient-file ./destination.recipient"},
	},
	"profile import": {
		{Purpose: "Preview archive into a new profile", Command: "devtools profile import --file ./my-app.age --as restored-app"},
		{Purpose: "Apply that preview", Command: "devtools profile import --file ./my-app.age --as restored-app --apply DIGEST"},
	},
	"import": {
		{Purpose: "Preview dotenv import", Command: "devtools import --file ./.env.local --dry-run"},
		{Purpose: "Import with an explicit public variable", Command: "devtools import --file ./.env.local --var API_URL"},
	},
	"dashboard": {
		{Purpose: "Open local editing dashboard", Command: "devtools dashboard"},
		{Purpose: "Initial selected profile", Command: "devtools dashboard --profile my-app"},
	},
	"dashboard status": {
		{Purpose: "Inspect server without login issuance", Command: "devtools dashboard status"},
	},
	"dashboard stop": {
		{Purpose: "Stop local dashboard server", Command: "devtools dashboard stop"},
	},
	"task sync": {
		{Purpose: "Acknowledge current definition for this run", Command: "devtools task sync TASK_ID --if-revision PROFILE_REVISION --reason \"Reviewed definition changes\""},
	},
	"task workstream edit": {
		{Purpose: "Preview atomic definition changes", Command: "devtools task workstream edit WS_ID --if-revision PROFILE_REVISION --file ./workstream-edit.json --dry-run"},
		{Purpose: "Apply reviewed changes", Command: "devtools task workstream edit WS_ID --if-revision PROFILE_REVISION --file ./workstream-edit.json"},
	},
	"task workstream create": {
		{Purpose: "Create a draft", Command: "devtools task workstream create --title \"CLI output improvements\""},
		{Purpose: "Create with full description", Command: "devtools task workstream create --file ./workstream.json"},
	},
	"task workstream update": {
		{Purpose: "Change inspected title", Command: "devtools task workstream update WS_ID --if-revision PROFILE_REVISION --title \"Clear CLI output\""},
	},
	"task add": {
		{Purpose: "Standalone task", Command: "devtools task add --title \"Review CLI errors\""},
		{Purpose: "Task in existing workstream", Command: "devtools task add --title \"Review CLI errors\" --workstream WS_ID --acceptance-keys A01"},
	},
	"task update": {
		{Purpose: "Change inspected task title", Command: "devtools task update TASK_ID --if-revision PROFILE_REVISION --title \"Review prerequisite errors\""},
		{Purpose: "Typed definition patch", Command: "devtools task update TASK_ID --if-revision PROFILE_REVISION --file ./task-patch.json"},
	},
	"task workstream spec set": {
		{Purpose: "Replace complete specification", Command: "devtools task workstream spec set WS_ID --if-revision PROFILE_REVISION --file ./spec.json"},
	},
	"task workstream plan set": {
		{Purpose: "Replace plan and complete membership", Command: "devtools task workstream plan set WS_ID --if-revision PROFILE_REVISION --file ./plan.json"},
	},
	"task depends set": {
		{Purpose: "Set a different prerequisite", Command: "devtools task depends set TASK_ID --depends-on PREREQ_TASK_ID --if-revision PROFILE_REVISION"},
		{Purpose: "Clear prerequisites with an empty array", Command: "devtools task depends set TASK_ID --file ./empty-dependencies.json --if-revision PROFILE_REVISION"},
	},
	"task workstream depends set": {
		{Purpose: "Set workstream prerequisite", Command: "devtools task workstream depends set WS_ID --depends-on PREREQ_WS_ID --if-revision PROFILE_REVISION"},
	},
	"task attach": {
		{Purpose: "Attach existing task", Command: "devtools task attach TASK_ID --workstream WS_ID --if-revision PROFILE_REVISION"},
	},
	"task detach": {
		{Purpose: "Detach without deleting task", Command: "devtools task detach TASK_ID --if-revision PROFILE_REVISION"},
	},
	"task hold": {
		{Purpose: "Record why work is held", Command: "devtools task hold TASK_ID --reason \"Waiting for service access\" --if-revision PROFILE_REVISION"},
	},
	"task unhold": {
		{Purpose: "Remove explicit hold", Command: "devtools task unhold TASK_ID --if-revision PROFILE_REVISION"},
	},
	"task cancel": {
		{Purpose: "Cancel with reason", Command: "devtools task cancel TASK_ID --reason \"Scope removed\" --if-revision PROFILE_REVISION"},
	},
	"task reopen": {
		{Purpose: "Reopen inspected task", Command: "devtools task reopen TASK_ID --reason \"Additional work required\" --if-revision PROFILE_REVISION"},
	},
	"task workstream cancel": {
		{Purpose: "Cancel inspected workstream", Command: "devtools task workstream cancel WS_ID --reason \"Project deferred\" --if-revision PROFILE_REVISION"},
	},
	"task workstream reopen": {
		{Purpose: "Reopen for further work", Command: "devtools task workstream reopen WS_ID --reason \"Scope extended\" --if-revision PROFILE_REVISION"},
	},
	"task workstream activate": {
		{Purpose: "Activate after structural check", Command: "devtools task workstream activate WS_ID --if-revision PROFILE_REVISION"},
	},
	"task workstream close": {
		{Purpose: "Close with current completion evidence", Command: "devtools task workstream close WS_ID --if-revision PROFILE_REVISION"},
	},
	"task claim": {
		{Purpose: "Claim explicit ready task", Command: "devtools task claim TASK_ID"},
		{Purpose: "Choose next ready task in one workstream", Command: "devtools task claim --workstream WS_ID"},
		{Purpose: "Choose next ready task in current profile", Command: "devtools task claim"},
	},
	"task resume": {
		{Purpose: "Resume the current context's run", Command: "devtools task resume"},
	},
	"task takeover": {
		{Purpose: "Replace observed active run", Command: "devtools task takeover TASK_ID --expected-run RUN_ID --dir ./app"},
	},
	"task unclaim": {
		{Purpose: "Revoke only observed run", Command: "devtools task unclaim TASK_ID --expected-run RUN_ID --if-revision PROFILE_REVISION --reason \"Owner handoff\""},
	},
	"task checkpoint": {
		{Purpose: "Save current progress for run", Command: "devtools task checkpoint RUN_ID --summary \"Root causes identified\" --next-action \"Review remaining commands\""},
		{Purpose: "Save structured handoff details", Command: "devtools task checkpoint RUN_ID --file ./checkpoint.json"},
	},
	"task release": {
		{Purpose: "Release without completing task", Command: "devtools task release RUN_ID --summary \"Investigation checkpointed\" --remaining \"Native verification\""},
	},
	"task done": {
		{Purpose: "Complete with actual validation record", Command: "devtools task done TASK_ID --summary \"Accepted behavior verified\" --validation-record-ids RECORD_ID"},
	},
	"task validation add": {
		{Purpose: "Task-owned check", Command: "devtools task validation add --title \"Prerequisite diagnosis\" --method \"Run the isolated failure fixture\" --task TASK_ID --acceptance-keys A01"},
		{Purpose: "Workstream-owned check", Command: "devtools task validation add --title \"CLI acceptance\" --method \"Review final installed behavior\" --workstream WS_ID --acceptance-keys A01"},
	},
	"task validation update": {
		{Purpose: "Change inspected method", Command: "devtools task validation update VALIDATION_ID --if-revision PROFILE_REVISION --method \"Verify known tools during a busy claim\""},
	},
	"task validation basis": {
		{Purpose: "Current task-run basis", Command: "devtools task validation basis VALIDATION_ID --file ./basis.json"},
		{Purpose: "Workstream basis at inspected revision", Command: "devtools task validation basis VALIDATION_ID --if-revision PROFILE_REVISION --file ./basis.json"},
	},
	"task validation record": {
		{Purpose: "Record actual result and evidence", Command: "devtools task validation record VALIDATION_ID --file ./validation-result.json"},
	},
	"task validation accept": {
		{Purpose: "Reuse observed record for current basis", Command: "devtools task validation accept VALIDATION_ID --basis BASIS_ID --record RECORD_ID --reason \"Same relevant code and conditions\""},
	},
	"task validation waive": {
		{Purpose: "Waive with explicit reason", Command: "devtools task validation waive VALIDATION_ID --reason \"Accepted native coverage limitation\" --if-revision PROFILE_REVISION"},
	},
	"task validation unwaive": {
		{Purpose: "Require validation again", Command: "devtools task validation unwaive VALIDATION_ID --reason \"Native environment available\" --if-revision PROFILE_REVISION"},
	},
	"task list": {
		{Purpose: "Default active work", Command: "devtools task list"},
		{Purpose: "One workstream", Command: "devtools task list --workstream WS_ID"},
		{Purpose: "Include all lifecycle, completion and membership states", Command: "devtools task list --state all --completion all --scope all"},
	},
	"task show": {
		{Purpose: "One task's definition and state", Command: "devtools task show TASK_ID"},
	},
	"task next": {
		{Purpose: "Next ready task in profile", Command: "devtools task next"},
		{Purpose: "One workstream", Command: "devtools task next --workstream WS_ID"},
	},
	"task current": {
		{Purpose: "Current directory runs", Command: "devtools task current"},
		{Purpose: "Another execution directory", Command: "devtools task current --dir ./app"},
	},
	"task context": {
		{Purpose: "Full public task context", Command: "devtools task context TASK_ID"},
	},
	"task history": {
		{Purpose: "Profile events", Command: "devtools task history"},
		{Purpose: "One task's events", Command: "devtools task history TASK_ID --limit 20"},
		{Purpose: "Continue same task query", Command: "devtools task history TASK_ID --limit 20 --cursor CURSOR"},
	},
	"task impact": {
		{Purpose: "Current downstream impact", Command: "devtools task impact TASK_ID"},
		{Purpose: "Impact of proposed definition", Command: "devtools task impact TASK_ID --file ./task-impact.json"},
	},
	"task tree": {
		{Purpose: "Prerequisites of one task", Command: "devtools task tree TASK_ID"},
		{Purpose: "Dependent work", Command: "devtools task tree TASK_ID --direction downstream"},
	},
	"task export": {
		{Purpose: "Complete public task export as JSON", Command: "devtools --format json task export TASK_ID"},
	},
	"task checkpoint list": {
		{Purpose: "Saved progress for run", Command: "devtools task checkpoint list RUN_ID"},
		{Purpose: "Continue same run query", Command: "devtools task checkpoint list RUN_ID --limit 20 --cursor CURSOR"},
	},
	"task workstream list": {
		{Purpose: "Default workstreams", Command: "devtools task workstream list"},
		{Purpose: "Include done and canceled workstreams", Command: "devtools task workstream list --state all --completion all"},
	},
	"task workstream show": {
		{Purpose: "Definition and status", Command: "devtools task workstream show WS_ID"},
	},
	"task workstream context": {
		{Purpose: "Full public workstream context", Command: "devtools task workstream context WS_ID"},
	},
	"task workstream history": {
		{Purpose: "Profile events", Command: "devtools task workstream history"},
		{Purpose: "One workstream's events", Command: "devtools task workstream history WS_ID --limit 20"},
		{Purpose: "Continue that query", Command: "devtools task workstream history WS_ID --limit 20 --cursor CURSOR"},
	},
	"task workstream impact": {
		{Purpose: "Current downstream impact", Command: "devtools task workstream impact WS_ID"},
		{Purpose: "Proposed workstream changes", Command: "devtools task workstream impact WS_ID --file ./workstream-impact.json"},
	},
	"task workstream tree": {
		{Purpose: "Prerequisite workstreams", Command: "devtools task workstream tree WS_ID"},
		{Purpose: "Dependent workstreams", Command: "devtools task workstream tree WS_ID --direction downstream"},
	},
	"task workstream export": {
		{Purpose: "Complete public workstream export as JSON", Command: "devtools --format json task workstream export WS_ID"},
	},
	"task workstream check": {
		{Purpose: "Structure, execution and closure readiness", Command: "devtools task workstream check WS_ID"},
	},
	"task workstream spec show": {
		{Purpose: "Requested specification", Command: "devtools task workstream spec show WS_ID"},
	},
	"task workstream plan show": {
		{Purpose: "Current plan", Command: "devtools task workstream plan show WS_ID"},
		{Purpose: "Plan at observed historical revision", Command: "devtools task workstream plan show WS_ID --at-revision PROFILE_REVISION"},
	},
	"task validation list": {
		{Purpose: "Current included definitions", Command: "devtools task validation list"},
		{Purpose: "One workstream", Command: "devtools task validation list --workstream WS_ID"},
		{Purpose: "Include removed definitions", Command: "devtools task validation list --scope all"},
	},
	"task validation show": {
		{Purpose: "Definition, current basis and evidence", Command: "devtools task validation show VALIDATION_ID"},
	},
}
