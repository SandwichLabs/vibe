// Screens 05-06: connections rail, connection editor.
import { icon, page, artboard, shell, TREE_FULL, treeFull, ctype, scope, SCOPE_LABEL, st, tn, connRow, datasheet, ORDERS, STATUS, TYPES } from './lib.mjs'

const tableMain = () => `
<div class="page-subhead" style="padding:12px 24px">
  <div><div class="crumb">local <span class="sep">&rsaquo;</span> Table</div>
    <div class="row" style="gap:10px;margin-top:4px"><h1 style="font-size:22px;font-family:var(--sh-font-mono)">orders</h1><span class="badge badge--teal"><span class="dot"></span>Editable</span><span class="help" style="font-family:var(--sh-font-mono)">48,213 rows</span></div></div>
  <span class="spacer"></span>
  <button class="btn btn-sm" type="button">${icon('plus', 14)} Add row</button>
  <button class="btn btn-sm" type="button">${icon('filter', 14)} Filter</button>
  <button class="btn btn-sm" type="button">${icon('download', 14)} Export</button>
</div>
<div class="scroll-pane">${datasheet(ORDERS)}</div>`

const rowCtx = `<div class="menu" role="menu" aria-label="prod_pg actions" style="left:292px;top:392px;width:262px">
  <div class="mhead">prod_pg &middot; Postgres</div>
  <div class="mi" role="menuitem">${icon('unlock')}<span class="grow">Disconnect</span></div>
  <div class="mi" role="menuitem">${icon('refresh')}<span class="grow">Refresh</span><span class="end"><kbd>F5</kbd></span></div>
  <div class="mi" role="menuitem">${icon('terminal')}<span class="grow">New query here</span><span class="end"><kbd>Ctrl Alt N</kbd></span></div>
  <div class="msep"></div>
  <div class="mi" role="menuitem">${icon('edit')}<span class="grow">Edit&hellip;</span><span class="end"><kbd>Enter</kbd></span></div>
  <div class="mi" role="menuitem">${icon('zap')}<span class="grow">Test</span></div>
  <div class="mi" role="menuitem">${icon('copy')}<span class="grow">Duplicate</span></div>
  <div class="msep"></div>
  <div class="mi danger" role="menuitem">${icon('trash')}<span class="grow">Remove&hellip;</span><span class="end"><kbd>Del</kbd></span></div>
  <div class="mnote">Shows Connect instead of Disconnect when it isn't connected.</div>
</div>`

const treeCtx = [
  connRow({ name: 'local', type: 'duckdb', scopeName: 'shared', status: 'ok', chev: 'closed' }),
  connRow({ name: 'prod_pg', type: 'postgres', scopeName: 'shared', status: 'ok', chev: 'closed', ro: true, sel: true }),
  connRow({ name: 'warehouse', type: 'postgres', scopeName: 'local', status: 'creds', chev: 'closed', shadow: true }),
  connRow({ name: 'md', type: 'motherduck', scopeName: 'user', status: 'creds', chev: 'closed', ro: true }),
  connRow({ name: 'lake', type: 's3', scopeName: 'shared', status: 'ext', chev: null, ro: true }),
].join('\n')

// A tree with more detail expanded: used in the close-up.
const treeDetail = [
  connRow({ name: 'prod_pg', type: 'postgres', scopeName: 'shared', status: 'ok', chev: 'open', ro: true }),
  tn({ d: 1, chev: 'open', ico: 'layers', label: 'public' }),
  tn({ d: 2, chev: 'open', ico: 'tableIcon', label: 'orders', meta: '~2.1M' }),
  tn({ d: 3, ico: 'key', label: 'id', meta: 'BIGINT &middot; PK', metaCls: 'pk' }),
  tn({ d: 3, ico: 'link', label: 'customer_id', meta: 'BIGINT &middot; FK', metaCls: 'fk' }),
  tn({ d: 3, label: 'status', meta: 'VARCHAR', metaCls: 'type', ico: 'columns' }),
  tn({ d: 3, label: 'total', meta: 'DECIMAL(10,2)', metaCls: 'type', ico: 'columns' }),
  tn({ d: 2, chev: 'closed', ico: 'tableIcon', label: 'customers', meta: '~310K' }),
  tn({ d: 2, ico: 'eye', label: 'v_open_orders', meta: 'view' }),
  tn({ d: 1, chev: 'closed', ico: 'layers', label: 'reporting' }),
  connRow({ name: 'staging_pg', type: 'postgres', scopeName: 'local', status: 'err', chev: 'closed' }),
  `<div class="tn-note err" role="note">Can't connect: password was rejected. <a href="#" style="color:inherit">Fix</a></div>`,
  connRow({ name: 'reports_pg', type: 'postgres', scopeName: 'shared', status: 'creds', chev: 'closed', ro: true }),
  `<div class="tn-note warn" role="note">The server changed since you signed in (db.internal to db-new.internal). <a href="#" style="color:inherit">Sign in again</a></div>`,
  connRow({ name: 'md', type: 'motherduck', scopeName: 'user', status: 'creds', chev: 'closed', ro: true }),
  `<div class="tn-note warn" role="note">Needs a MotherDuck token. <a href="#" style="color:inherit">Add it</a></div>`,
  connRow({ name: 'lake', type: 's3', scopeName: 'shared', status: 'ext', chev: null, ro: true }),
  `<div class="tn-note" role="note">Needs the httpfs extension. <a href="#" style="color:inherit">Install&hellip;</a></div>`,
  connRow({ name: 'ops_db', type: 'duckdb', scopeName: 'shared', status: 'lock', chev: null, statusLabel: 'In use by vibe mcp serve (pid 4182)' }),
  `<div class="tn-note" role="note">In use by <code>vibe mcp serve</code> (pid 4182). <a href="#" style="color:inherit">Take over</a></div>`,
].join('\n')

const emptyRail = [
  connRow({ name: 'local', type: 'duckdb', scopeName: 'shared', status: 'ok', chev: 'open' }),
  `<div class="tn-note" style="padding-left:34px" role="note">No tables yet. Import a CSV, or ask the agent to build one.</div>`,
  `<div class="rail-empty"><b>Your data somewhere else?</b>Connect Postgres, MotherDuck, a SQLite file or cloud storage. Nothing connects until you use it.<div style="margin-top:10px"><button class="btn btn-sm" type="button">${icon('plus', 14)} Add a connection</button></div></div>`,
].join('\n')

const firstRunMain = () => `
<div class="empty-hero" style="padding-top:64px">
  <span class="glyph">${icon('database')}</span>
  <h2>Let's get some data in here</h2>
  <p class="lead">Start with a file you already have, or point Vibe at a database. You can do both.</p>
  <div class="row" style="gap:14px"><button class="btn btn-primary" type="button">${icon('download')} Import a CSV or Excel file</button><button class="btn" type="button">${icon('plug')} Add a connection</button><button class="btn btn-ghost" type="button">${icon('bot')} Ask the agent</button></div>
</div>`

const legend = () => `
<div class="page-pad legend-grid" style="overflow:visible">
  <div class="card" style="overflow:hidden">
    <table class="legend-table"><caption class="sr">Connection status icons</caption>
      <thead><tr><th scope="col">Status</th><th scope="col">What it means</th><th scope="col">Click or Enter</th></tr></thead>
      <tbody>
        <tr><th scope="row" class="k">${st('off')}Not connected</th><td>Nothing has been dialled. This is how every connection starts, so opening a workspace is instant.</td><td>Expands and connects.</td></tr>
        <tr><th scope="row" class="k">${st('ok')}Connected</th><td>Attached and answering.</td><td>Expands.</td></tr>
        <tr><th scope="row" class="k">${st('err')}Can't connect</th><td>The reason is shown under the row, with secrets removed. Never swallowed.</td><td>Opens the editor on the Test result.</td></tr>
        <tr><th scope="row" class="k">${st('creds')}Needs credentials</th><td>No saved sign-in on this computer yet, the keychain isn't reachable, or the server changed since you signed in (the sign-in is tied to the host, port and database). Sign in again to confirm.</td><td>Opens the sign-in prompt.</td></tr>
        <tr><th scope="row" class="k">${st('ext')}Extension needed</th><td>A one-time download is required for this type.</td><td>Opens the install prompt.</td></tr>
        <tr><th scope="row" class="k">${st('lock')}In use by pid 4182</th><td>Another Vibe process holds this local file (a people icon, so it isn't confused with the read-only padlock). Remote connections still work.</td><td>Offers Take over.</td></tr>
      </tbody>
    </table>
    <p class="help" style="padding:12px;border-top:1.5px solid var(--vibe-border-subtle)">If several apply, the row shows the first of: extension needed, needs credentials, can't connect, in use by another process, not connected, connected.</p>
  </div>
  <div class="col" style="gap:16px">
    <div class="card card-pad col" style="gap:12px"><div class="sh-label">Badge &middot; who gets this connection</div>
      <div class="row" style="gap:10px;align-items:flex-start">${scope('shared')}<span class="help">Everyone on this workspace. Saved in <code>vibe.yaml</code>, which git shares.</span></div>
      <div class="row" style="gap:10px;align-items:flex-start">${scope('local')}<span class="help">Only you, on this computer, for this workspace. <code>.vibe-local/</code>, never committed.</span></div>
      <div class="row" style="gap:10px;align-items:flex-start">${scope('user')}<span class="help">Only you, in every workspace. <code>~/.config/vibe/connections.yaml</code></span></div></div>
    <div class="card card-pad col" style="gap:10px"><div class="sh-label">Shadowing</div>
      <div class="tn conn" style="padding-left:4px;height:auto">${icon('chevronRight', 14, 'class="chev"')}${ctype('postgres')}<span class="nm" style="margin-left:6px">warehouse</span><span class="rowtail">${icon('layers', 12, 'class="ro" role="img" aria-label="Overrides a team connection"')}${scope('local')}${st('creds')}</span></div>
      <span class="help">The layered icon means this machine's private <code>warehouse</code> overrides a Team one of the same name. The most specific wins: This computer beats Team, and Team beats All my workspaces. Hover or open Edit to see both.</span>
      <div class="tn" style="padding-left:4px">${icon('lock', 12, 'class="ro" role="img" aria-label="Read-only"')}<span class="help" style="margin-left:6px">A padlock means the connection is read-only.</span></div>
    </div>
  </div>
</div>`

export const pageRail = () => page({
  num: '05', title: 'Connections rail',
  artboards: [
    artboard('A. In the shell', 'Focus ring drawn on the orders row (inset, so a clipped list cannot cut it). Nav on top, Connections explorer below it (replaces the Tables nav item and the Query Explorer\'s own schema tree). Below 1000px the nav shrinks to an icon rail and the tree becomes a drawer.',
      shell({ active: '', tree: treeFull({ sel: 'orders', focus: 'orders' }), main: tableMain(), peek: 'auto', treeAttrs: 'aria-multiselectable="false"' })),
    artboard('B. Close-up: schema, columns, row states', 'Hover reveals the row actions button. Detail under a row only appears when something needs doing.',
      `<div style="display:flex;justify-content:center;padding:24px;background:var(--sh-paper-2);height:100%"><div class="sidebar with-rail" style="width:296px;border:2px solid var(--vibe-border);height:100%;display:flex;flex-direction:column"><div class="conn-rail" style="border-top:0"><div class="rail-head"><div class="side-section">Connections</div><button class="icon-btn keep" type="button" aria-label="New connection">${icon('plus')}</button><button class="icon-btn" type="button" aria-label="Refresh">${icon('refresh')}</button></div><div class="tree" role="tree" aria-label="Connections">${treeDetail}</div></div></div></div>`, 'h-900'),
    artboard('C. Legend: status, scope, shadowing', 'Status is never colour alone: each state has its own shape, a text name and a tooltip.', legend(), 'h-700'),
    artboard('D. Context menu', 'Right-click, the row\'s ⋯ button, or Shift+F10 / the Menu key. Items depend on state.',
      shell({ active: '', tree: treeCtx, main: tableMain(), menu: rowCtx })),
    artboard('E. Empty states', 'Every workspace starts with local. First run points at the three ways in.',
      shell({ active: 'home', tree: emptyRail, main: firstRunMain() })),
  ],
})

// ---- editor ----
const typeCard = (t, name, desc, cls = '', soon = false) => `
<button class="card typecard${cls ? ' ' + cls : ''}${soon ? ' soon' : ''}" type="button" ${soon ? 'aria-disabled="true"' : ''}>${ctype(t, 'lg')}<span><span class="tt">${name}</span><span class="td">${desc}</span></span>${soon ? '<span class="badge badge--graphite">Coming soon</span>' : ''}</button>`

const pickerMain = () => `
<div class="page-pad" style="overflow:auto">
  <div class="headrow"><div><div class="crumb">Acme Analytics <span class="sep">/</span> Connections <span class="sep">/</span> New</div><h1>New connection</h1></div></div>
  <p class="help" style="font-size:15px;margin:-8px 0 20px">What are you connecting to? You'll sign in on the next step. Nothing is saved until you press Save.</p>
  <div class="type-grid" role="list">
    ${typeCard('duckdb', 'DuckDB file', 'A .duckdb file on this computer. Fast, and the best home for your own tables.')}
    ${typeCard('postgres', 'Postgres', 'A Postgres database on your network or in the cloud.', 'is-selected')}
    ${typeCard('motherduck', 'MotherDuck', 'DuckDB in the cloud. One MotherDuck login per workspace.')}
    ${typeCard('sqlite', 'SQLite file', 'A .sqlite or .db file, like the ones many apps keep.')}
    <button class="card typecard" type="button">${ctype('s3', 'lg')}<span><span class="tt">Amazon S3</span><span class="td">CSV, Parquet and JSON files in a bucket.</span></span></button>
    <button class="card typecard" type="button">${ctype('s3', 'lg').replace('S3', 'GC')}<span><span class="tt">Google Cloud Storage</span><span class="td">Files in a Cloud Storage bucket.</span></span></button>
    <button class="card typecard" type="button">${ctype('s3', 'lg').replace('S3', 'R2')}<span><span class="tt">Cloudflare R2</span><span class="td">Files in an R2 bucket.</span></span></button>
    ${typeCard('mysql', 'MySQL', 'Not available yet.', '', true)}
    ${typeCard('ducklake', 'DuckLake', 'Not available yet.', '', true)}
  </div>
  <div class="note info" style="margin-top:20px">${icon('info')}<span>Just have a CSV, Parquet or Excel file? You don't need a connection: put it in the workspace's <code>data/</code> folder or use Import, and query it from there.</span></div>
</div>`

const secretRow = (label, id) => `<div class="field"><label id="${id}-l">${label}</label>
  <div class="cred" role="group" aria-labelledby="${id}-l"><span class="dots" aria-label="Saved, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span><span class="where">${icon('shieldCheck')} In your keychain</span><span class="acts"><button class="linkbtn" type="button">Replace</button><span class="sepdot" aria-hidden="true">&middot;</span><button class="linkbtn danger" type="button">Clear</button></span></div></div>`
// Non-secret per-person fields (username, S3 key id) are readable and editable; only the secret half is write-only.
const userField = (value, label = 'Username') => `<div class="field"><label for="c-u">${label}</label><input id="c-u" class="field-input" value="${value}" autocomplete="off"><div class="help">Not a secret, so you can see and edit it. Saved on this computer.</div></div>`

const modeCards = (sel, pre = 'PG_PROD', md = false) => `
<div class="opt-row" role="radiogroup" aria-label="How you sign in">
  ${[['keychain', 'My keychain', 'Saved once on this computer. The default.'],
     ['env', 'Environment variables', md ? 'Read from <code>MOTHERDUCK_TOKEN</code> when Vibe starts. Good for scripts and servers.' : 'Read from <code>' + pre + '_USER</code> and <code>' + pre + '_PASSWORD</code> when Vibe starts. Good for scripts and servers.'],
     ['prompt', 'Ask me each time', 'Vibe asks when it connects and forgets it when you close the workspace.']].map(([k, t, d]) =>
    `<label class="opt-card${sel === k ? ' on' : ''}" style="cursor:pointer"><input type="radio" name="cm" ${sel === k ? 'checked' : ''}><span><span class="ot">${t}</span><span class="od">${d}</span></span></label>`).join('')}
</div>
<p class="help">The team's file only says which details are needed. <b>How you sign in is your own choice</b> and stays on this computer, so teammates can use a different way.</p>`

const scopeCards = (sel, editing = false) => `
<div class="opt-row" role="radiogroup" aria-label="Who gets this connection">
  ${[['local', 'This computer', 'Just you, in this workspace. Saved in <code>.vibe-local/</code>, never committed.'],
     ['shared', 'Team', 'Everyone on this workspace. Saved in <code>vibe.yaml</code>, so teammates get it when they pull.'],
     ['user', 'All my workspaces', 'Just you, in every workspace. Handy for your own MotherDuck or a personal database. Saved in <code>~/.config/vibe/</code>.']].map(([k, t, d]) =>
    `<label class="opt-card${sel === k ? ' on' : ''}" style="cursor:pointer"><input type="radio" name="sc" ${sel === k ? 'checked' : ''}><span><span class="ot">${t}</span><span class="od">${d}</span></span></label>`).join('')}
</div>
${sel === 'shared' ? `<div class="note" role="note">${icon('info')}<span><b>This file is committed to git.</b> The name, host, port and database are saved in <code>vibe.yaml</code>, so anyone who can see the repository can see them. Passwords and tokens are not saved there: they stay in each person's own keychain, and teammates enter their own. Your username is saved on this computer only.</span></div>` : ''}
${sel === 'local' ? `<div class="note" role="note">${icon('info')}<span>Saved on this computer only. It isn't committed and teammates won't see it. New connections start here; choose <b>Team</b> to share one.</span></div>` : ''}
${editing ? `<p class="help">You can move this later (<button class="linkbtn" type="button" style="padding:0 2px">Move to ${sel === 'shared' ? 'This computer' : 'Team'}</button>). The sign-in stays tied to this workspace, the connection's name and its server.</p>` : ''}`

const roSwitch = (on) => `
<div class="switch-row"><span class="toggle${on ? '' : ' off'}" role="switch" aria-checked="${on}" tabindex="0" aria-labelledby="ro-t" aria-describedby="ro-d"></span>
  <span><span class="st-t" id="ro-t">Read-only${on ? ' (recommended)' : ''}</span><span class="st-d" id="ro-d">${on
    ? 'Vibe will refuse to run changes on this connection, from you or the agent. This is a safety belt inside Vibe, not a lock on the database: a determined query can still get around it.'
    : 'Changes are allowed. You and the agent can edit data here. The agent will still ask you before every change.'}</span></span></div>
${on ? `<div class="note warn">${icon('alert')}<span><b>For production data, also use a read-only database user.</b> Ask your database admin for a role that can only read. That is the only way to be certain nothing can change.</span></div>` : ''}`

const testOk = `<div class="banner teal" role="status" style="margin:0 24px 20px"><div class="ico">${icon('check')}</div><div class="body"><div class="ttl">Connected in 0.4 seconds</div><div class="sub">Found 3 schemas and 41 tables. Vibe's read-only guard is on for this connection.</div></div></div>`
const testErr = `<div class="banner attn" role="alert" style="margin:0 24px 20px"><div class="ico">${icon('alert')}</div><div class="body"><div class="ttl">Couldn't connect</div><div class="sub" style="color:var(--sh-ink)">The server turned down the username or password. Check them and try again. If you're sure they're right, ask whoever runs the database whether your account can reach <code>db.staging.internal</code>.</div>
  <details style="margin-top:8px" class="help"><summary style="cursor:pointer;font-weight:600">Show details</summary><div class="details" style="margin-top:8px">Unable to connect to Postgres at user='rosa' password='[hidden]' host='db.staging.internal' port=5432: password authentication failed<br>Tested in 0.9 s &middot; passwords and tokens are removed from this text</div></details></div></div>`

const formCard = ({ name, fixedName = true, host, port, db, mode, credsHtml, ro, scopeSel, test, fail = false, adv = false, editing = false }) => `
<div class="card" style="box-shadow:var(--sh-shadow)">
  <div class="form-sec">
    <h3>Connection <span class="label">Postgres</span></h3>
    <div class="field"><label for="f-name">Name</label><input id="f-name" class="field-input" value="${name}" ${fixedName ? 'readonly aria-readonly="true"' : ''} style="font-family:var(--sh-font-mono)"><div class="help">${fixedName ? "The name can't change after it's created, because notebooks and queries refer to it. " : ''}How you'll refer to it in SQL: <code>${name}.public.orders</code>. Lowercase letters, numbers and underscores.</div></div>
    <div class="form-grid"><div class="field"><label for="f-host">Host</label><input id="f-host" class="field-input" value="${host}"></div><div class="field"><label for="f-port">Port</label><input id="f-port" class="field-input" value="${port}" inputmode="numeric"></div></div>
    <div class="field"><label for="f-db">Database</label><input id="f-db" class="field-input" value="${db}"><div class="help">If the host, port or database change, everyone is asked to sign in again to confirm.</div></div>
    <details ${adv ? 'open' : ''}><summary style="cursor:pointer;font-weight:600;font-size:14px">Advanced options</summary>
      <div class="field" style="margin-top:12px;max-width:260px"><label for="f-ssl">SSL mode</label><select id="f-ssl" class="field-input"><option>require</option><option>prefer</option><option>disable</option></select></div></details>
  </div>
  <div class="form-sec">
    <h3>Sign-in <span class="label">Just you &middot; this computer</span></h3>
    ${modeCards(mode, name.toUpperCase().replace('_PG','').replace(/^/, 'PG_'))}
    ${credsHtml}
  </div>
  <div class="form-sec"><h3>Safety</h3>${roSwitch(ro)}</div>
  <div class="form-sec"><h3>Who gets this connection</h3>${scopeCards(scopeSel, editing)}</div>
  <div class="test-bar"><button class="btn ${fail ? 'btn-primary' : ''}" type="button">${icon('zap')} ${fail ? 'Test again' : 'Test connection'}</button><span class="help">Stops after 10 seconds.</span><button class="btn" type="button">Cancel</button>${fail ? '<button class="btn" type="button">Save anyway</button>' : '<button class="btn btn-primary" type="button">Save</button>'}</div>
  ${test ?? ''}
</div>`

const editMain = (inner, { crumb, title, tile, badge, right = '' }) => `
<div class="page-pad" style="overflow:auto;height:100%">
  <div class="headrow"><div><div class="crumb">Acme Analytics <span class="sep">/</span> Connections <span class="sep">/</span> ${crumb}</div><div class="row" style="gap:14px;margin-top:2px">${tile}<h1 style="font-family:var(--sh-font-mono);font-size:30px">${title}</h1>${badge}</div></div><div class="row">${right}</div></div>
  <div style="max-width:860px">${inner}</div>
</div>`

const credInputs = `<div class="form-grid" style="grid-template-columns:1fr 1fr">
  ${userField('rosa')}
  <div class="field"><label for="c-p">Password</label><input id="c-p" class="field-input" type="password" value="hunter2hunter2" autocomplete="new-password"><div class="help">Saved to your keychain when you press Save. Vibe can't show it again, only replace or clear it.</div></div></div>
  <div class="note" role="note">${icon('info')}<span>Passwords and tokens go in your computer's keychain. They're never written to the workspace folder, to git, or to logs.</span></div>`

const credSaved = `<div class="form-grid" style="grid-template-columns:1fr 1fr">${userField('analytics_ro')}${secretRow('Password', 'cp')}</div>
  <div class="note" role="note">${icon('info')}<span>Passwords and tokens go in your computer's keychain. They're never written to the workspace folder, to git, or to logs.</span></div>`

const envPanel = `<div class="col" style="gap:8px">
  <div class="field"><label for="e-pre">Variable name prefix</label><input id="e-pre" class="field-input" value="PG_WAREHOUSE" style="font-family:var(--sh-font-mono);max-width:300px"></div>
  <div class="envvar">PG_WAREHOUSE_USER<span class="set yes">${icon('circleCheck')} Set</span></div>
  <div class="envvar">PG_WAREHOUSE_PASSWORD<span class="set no">${icon('alert')} Not set</span></div>
  <div class="help">Vibe only checks that the variables exist. It never shows their values. They must be set in the environment Vibe was started from, so restart Vibe after adding one.</div></div>`

const mdForm = `
<div class="card" style="box-shadow:var(--sh-shadow)">
  <div class="form-sec"><h3>Connection <span class="label">MotherDuck</span></h3>
    <div class="field"><label for="m-name">Name</label><input id="m-name" class="field-input" value="md" style="font-family:var(--sh-font-mono)"></div>
    <div class="field"><label for="m-db">Database</label><input id="m-db" class="field-input" value="my_db"><div class="help">One database per connection. Add the connection again for another one.</div></div>
  </div>
  <div class="form-sec"><h3>Sign-in <span class="label">Just you &middot; this computer</span></h3>
    ${modeCards('keychain', '', true)}
    <div class="field"><label for="m-t">MotherDuck token</label><input id="m-t" class="field-input" type="password" placeholder="Paste your token" autocomplete="off"><div class="help">Find it in MotherDuck under Settings. It's saved to your keychain and Vibe can't show it again.</div></div>
    <div class="note warn">${icon('alert')}<span><b>One MotherDuck login per workspace.</b> To use a different login, open a different workspace. Vibe restarts when you switch between logins.</span></div></div>
  <div class="form-sec"><h3>Safety</h3>${roSwitch(true)}</div>
  <div class="form-sec"><h3>Who gets this connection</h3>${scopeCards('user')}</div>
  <div class="test-bar"><button class="btn" type="button">${icon('zap')} Test connection</button><span class="help">Tested in a separate process.</span><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Save</button></div>
</div>`

export const pageEditor = () => page({
  num: '06', title: 'Connection editor',
  artboards: [
    artboard('A. Choose a type', 'Step 1. The grid is generated from the type registry; types without a driver yet are visible but not clickable (aria-disabled).',
      shell({ active: '', tree: treeFull(), main: pickerMain() })),
    artboard('B. Edit an existing connection (sign-in saved, test passed)', 'Step 2, generated from the type\'s field list. The username is a normal field; the password is write-only: Replace or Clear, never reveal.',
      shell({ active: '', tree: treeFull({ sel: 'prod_pg' }), main: editMain(formCard({ name: 'prod_pg', host: 'db.internal', port: '5432', db: 'analytics', mode: 'keychain', credsHtml: credSaved, ro: true, scopeSel: 'shared', test: testOk, editing: true }), { crumb: 'prod_pg', title: 'prod_pg', tile: ctype('postgres', 'lg'), badge: '<span class="badge badge--success"><span class="dot"></span>Connected</span>', right: '<button class="btn btn-destructive btn-sm" type="button">' + icon('trash', 14) + ' Remove&hellip;</button>' }) }), 'h-1600'),
    artboard('C. New connection, test failed', 'New connections start on "This computer". Errors are plain and redacted (the user name stays, the password is hidden); details are one click away. Saving anyway stays available.',
      shell({ active: '', tree: treeFull({ drop: ['staging_pg'] }), main: editMain(formCard({ name: 'staging_pg', fixedName: false, host: 'db.staging.internal', port: '5432', db: 'analytics', mode: 'keychain', credsHtml: credInputs, ro: true, scopeSel: 'local', test: testErr, fail: true, adv: true }), { crumb: 'New', title: 'staging_pg', tile: ctype('postgres', 'lg'), badge: '<span class="badge badge--error"><span class="dot"></span>Test failed</span>' }) }), 'h-1600'),
    artboard('D. Private copy that overrides a Team connection, sign-in from environment variables', 'Shadowing explained in place. Env variable names shown with Set / Not set; values never shown. One variable is missing, so the rail shows Needs credentials.',
      shell({ active: '', tree: treeFull({ sel: 'warehouse' }), main: editMain(`<div class="note warn" style="margin-bottom:16px" role="note">${icon('layers')}<span><b>This is your private copy of warehouse.</b> On this computer it overrides the Team <code>warehouse</code> in <code>vibe.yaml</code>. <button class="linkbtn" type="button" style="padding:0 4px">Look at the Team one</button> <button class="linkbtn danger" type="button" style="padding:0 4px">Remove my copy</button></span></div>` + formCard({ name: 'warehouse', host: 'localhost', port: '15432', db: 'warehouse', mode: 'env', credsHtml: envPanel, ro: false, scopeSel: 'local', test: '', editing: true }), { crumb: 'warehouse', title: 'warehouse', tile: ctype('postgres', 'lg'), badge: '<span class="badge badge--warn"><span class="dot"></span>Needs credentials</span>' }) }), 'h-1600'),
    artboard('E. MotherDuck', 'Same shell, different generated fields: token only, no host or port. Keychain, environment variable (MOTHERDUCK_TOKEN) or ask each time, like every type.',
      shell({ active: '', tree: treeFull(), main: editMain(mdForm, { crumb: 'New', title: 'md', tile: ctype('motherduck', 'lg'), badge: '' }) }), 'h-1600'),
  ],
})
