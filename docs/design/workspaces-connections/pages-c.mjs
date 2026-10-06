// Screens 07-10: states, query editor / SQL cell, settings, agent approval.
import { icon, page, artboard, shell, TREE_FULL, treeFull, ctype, scope, st, tn, connRow, datasheet, ORDERS } from './lib.mjs'

const homeStub = () => `<div class="page-pad"><div class="headrow"><div><div class="crumb">Acme Analytics <span class="sep">/</span> Home</div><h1>Welcome back</h1></div></div>
<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:16px">${['Weekly revenue', 'Churn watchlist', 'Inventory check'].map((n) => `<div class="card card-pad"><div class="sh-label">Notebook</div><div style="font-family:var(--sh-font-display);font-size:20px;font-weight:600;margin:6px 0">${n}</div></div>`).join('')}</div></div>`

// ---- 07 states ---------------------------------------------------------
const needsCreds = () => `
<div class="modal-overlay" role="presentation"><div class="dialog" role="dialog" aria-modal="true" aria-labelledby="nc-h">
  <div class="d-head"><h2 id="nc-h">Sign in to prod_pg</h2><p>Vibe doesn't have a username and password for this connection on this computer yet.</p></div>
  <div class="d-body">
    <div class="field"><label for="nc-u">Username</label><input id="nc-u" class="field-input" autocomplete="off" autofocus></div>
    <div class="field"><label for="nc-p">Password</label><input id="nc-p" class="field-input" type="password" autocomplete="new-password"></div>
    <div class="switch-row"><input type="checkbox" id="nc-r" checked><label for="nc-r"><span class="st-t">Remember in my keychain</span><span class="st-d">Saved on this computer only. Vibe can't show it again, only replace or clear it.</span></label></div>
  </div>
  <div class="d-foot"><span class="left">Set to ask each time? This box starts unticked.</span><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Connect</button></div>
</div></div>`

const fingerprint = () => `
<div class="modal-overlay" role="presentation"><div class="dialog" role="dialog" aria-modal="true" aria-labelledby="fp-h">
  <div class="d-head"><h2 id="fp-h">Sign in to prod_pg again</h2><p>prod_pg's server changed since you signed in (<code>db.internal</code> to <code>db-new.internal</code>). Sign in again to confirm it's the one you mean.</p></div>
  <div class="d-body">
    <div class="note warn">${icon('alert')}<span>This usually happens after you pull a change from your team. Your saved password is only used with the server it was saved for, so it isn't sent to the new one.</span></div>
    <div class="field"><label for="fp-u">Username</label><input id="fp-u" class="field-input" value="analytics_ro" autocomplete="off"></div>
    <div class="field"><label for="fp-p">Password</label><input id="fp-p" class="field-input" type="password" autocomplete="new-password" autofocus></div>
  </div>
  <div class="d-foot"><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Confirm and connect</button></div>
</div></div>`

const keychainDown = () => `
<div class="modal-overlay" role="presentation"><div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="kc-h" aria-describedby="kc-d">
  <div class="d-head"><h2 id="kc-h">Can't reach your keychain</h2></div>
  <div class="d-body" id="kc-d">
    <p>Vibe stores passwords in this computer's keychain, but it isn't answering. That's common over SSH or when no one is signed in to the desktop.</p>
    <div class="note info">${icon('info')}<span>You can still connect. Set <code>PG_PROD_USER</code> and <code>PG_PROD_PASSWORD</code> in the environment Vibe starts from, then choose <b>Environment variables</b> for this connection. Vibe never writes passwords to a file instead.</span></div>
  </div>
  <div class="d-foot"><button class="btn" type="button">Try again</button><button class="btn btn-primary" type="button">Edit connection</button></div>
</div></div>`

const extConsent = () => `
<div class="modal-overlay" role="presentation"><div class="dialog" role="dialog" aria-modal="true" aria-labelledby="ex-h">
  <div class="d-head"><h2 id="ex-h">Install the httpfs extension?</h2></div>
  <div class="d-body">
    <p>To read files from S3, Google Cloud Storage or R2, Vibe's database engine needs a small add-on called the <code>httpfs</code> extension. It isn't included, so Vibe needs to download it once. (Postgres connections ask the same way for the <code>postgres</code> extension.)</p>
    <ul role="list" style="list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px;font-size:14px">
      <li class="row" style="gap:10px;align-items:flex-start">${icon('download', 16, 'style="flex:none;margin-top:3px;color:var(--sh-graphite)"')}<span>Downloaded from <code>extensions.duckdb.org</code>.</span></li>
      <li class="row" style="gap:10px;align-items:flex-start">${icon('hdd', 16, 'style="flex:none;margin-top:3px;color:var(--sh-graphite)"')}<span>Kept in Vibe's own folder, shared by all your workspaces. It won't touch any DuckDB you installed yourself.</span></li>
      <li class="row" style="gap:10px;align-items:flex-start">${icon('shieldCheck', 16, 'style="flex:none;margin-top:3px;color:var(--sh-graphite)"')}<span>Needed by 1 connection here: <code>lake</code>.</span></li>
    </ul>
    <div class="note warn" role="note">${icon('alert')}<span>Offline? You can skip this and the connection will wait with an "Extension needed" icon.</span></div>
  </div>
  <div class="d-foot"><button class="btn" type="button">Not now</button><button class="btn btn-primary" type="button">${icon('download')} Install httpfs extension</button></div>
</div></div>`

const roTree = [
  connRow({ name: 'local', type: 'duckdb', scopeName: 'shared', status: 'lock', chev: null, statusLabel: 'In use by vibe mcp serve (pid 4182)' }),
  `<div class="tn-note" role="note" style="padding-left:30px">In use by <code>vibe mcp serve</code> (pid 4182). Shown, not editable.</div>`,
  connRow({ name: 'prod_pg', type: 'postgres', scopeName: 'shared', status: 'ok', chev: 'closed', ro: true }),
  connRow({ name: 'warehouse', type: 'postgres', scopeName: 'local', status: 'creds', chev: 'closed', shadow: true }),
  connRow({ name: 'md', type: 'motherduck', scopeName: 'user', status: 'creds', chev: 'closed', ro: true }),
  connRow({ name: 'lake', type: 's3', scopeName: 'shared', status: 'ext', chev: null, ro: true }),
].join('\n')

const roBanner = `
<div class="ws-banner" role="status">${icon('lock', 20, 'class="lead-ico"')}
  <div class="bt"><b>Opened read-only.</b> <code>vibe mcp serve</code> (pid 4182) is already running this workspace, so Vibe can look at it but not change it.
    <span class="sub">Notebooks, queries and skills are plain files and stay editable, and remote connections work. Local database files are in use and can't be edited until it lets go.</span></div>
  <div class="row"><button class="btn btn-sm btn-primary" type="button">Take over&hellip;</button><button class="btn btn-sm" type="button">Keep read-only</button></div></div>`

const takeOver = () => `
<div class="modal-overlay" role="presentation"><div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="to-h" aria-describedby="to-d">
  <div class="d-head"><h2 id="to-h">Take over Acme Analytics?</h2></div>
  <div class="d-body" id="to-d">
    <p>Vibe will ask <code>vibe mcp serve</code> (pid 4182) to let go of this workspace, then open it for editing.</p>
    <p>Claude Desktop or Claude Code sessions that were using it will reconnect through Vibe Studio. Nothing is deleted, and open notebooks aren't affected.</p>
    <div class="note info">${icon('info')}<span>After you take over, any change Claude makes needs your approval in Vibe Studio, and closing Studio ends the session it was passing along.</span></div>
  </div>
  <div class="d-foot"><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Take over</button></div>
</div></div>`

const conflictMain = () => `
<div class="ws-banner attn" role="alert">${icon('alert', 20, 'class="lead-ico" style="color:var(--sh-coral)"')}
  <div class="bt"><b>This notebook changed on disk.</b> <code>notebooks/weekly-revenue.vnb</code> was saved by something else while you had it open: a git pull, another editor, or the agent.
    <span class="sub">Your unsaved edit (1 cell) and the new version differ in 2 cells. Pick one. Nothing happens until you do.</span></div>
  <div class="row"><button class="btn btn-sm btn-primary" type="button">${icon('refresh', 14)} Reload from disk</button><button class="btn btn-sm" type="button">Keep mine</button><button class="btn btn-sm" type="button">Save mine as a copy</button></div></div>
<div class="nb-wrap">
  <div class="headrow" style="margin-bottom:0"><div><div class="crumb">Notebooks <span class="sep">/</span> weekly-revenue</div><h1>Weekly revenue</h1></div></div>
  ${sqlCell({ k: 'SQL &middot; revenue by week', conn: 'local', code: `<span class="k">SELECT</span> date_trunc(<span class="s">'week'</span>, ordered_at) <span class="k">AS</span> week,\n       <span class="k">sum</span>(total) <span class="k">AS</span> revenue\n<span class="k">FROM</span> orders <span class="k">GROUP BY</span> 1 <span class="k">ORDER BY</span> 1 <span class="k">DESC</span>;`, edited: true })}
  <div class="cell-card"><div class="cell-head"><span class="kind">Text</span></div><div style="padding:14px 16px;font-size:14px;line-height:1.6">Revenue by week for the last quarter. The big dip in August is the supplier outage.</div></div>
</div>`

const sqlCell = ({ k, conn, connType = 'duckdb', code, edited = false, explicit = false, menu = '', result = '' }) => `
<div class="cell-card" style="position:relative"><div class="cell-head"><span class="kind">${k}</span>${edited ? '<span class="badge badge--warn" style="padding:1px 8px;font-size:11px"><span class="dot"></span>Edited</span>' : ''}<span class="spacer"></span>
  <button class="conn-select cell" type="button" aria-haspopup="listbox" aria-label="Run on: ${conn}${explicit ? '' : ' (workspace default)'}">${ctype(connType)}${explicit ? conn : `Default &middot; ${conn}`}${icon('chevronDown', 14, 'class="cv"')}</button>
  <button class="btn btn-sm btn-primary" type="button">${icon('play', 14)} Run</button></div>
  <pre class="sqlbox">${code}</pre>${result}${menu}</div>`

const roSheetMain = () => `
<div class="ws-banner" style="background:var(--sh-teal-8)" role="note">${icon('lock', 20, 'class="lead-ico" style="color:var(--sh-teal)"')}
  <div class="bt"><b>This table is read-only.</b> <code>prod_pg</code> is set to read-only, so rows can't be added, edited or deleted here.
    <span class="sub">To change data, turn off Read-only for the connection. For production data, use a database user that is allowed to write.</span></div>
  <div class="row"><button class="btn btn-sm" type="button">${icon('edit', 14)} Edit connection</button></div></div>
<div class="page-subhead" style="padding:12px 24px">
  <div><div class="crumb">prod_pg <span class="sep">&rsaquo;</span> public <span class="sep">&rsaquo;</span> Table</div>
    <div class="row" style="gap:10px;margin-top:4px"><h1 style="font-size:22px;font-family:var(--sh-font-mono)">orders</h1><span class="badge badge--graphite ro-badge">${icon('lock', 12)} Read-only</span><span class="help" style="font-family:var(--sh-font-mono)">~2.1M rows &middot; showing first 1,000</span></div></div>
  <span class="spacer"></span>
  <button class="btn btn-sm" type="button" disabled title="prod_pg is read-only">${icon('plus', 14)} Add row</button>
  <button class="btn btn-sm" type="button">${icon('filter', 14)} Filter</button>
  <button class="btn btn-sm" type="button">${icon('download', 14)} Export</button>
</div>
<div class="scroll-pane lockcell">${datasheet({ ...ORDERS, ro: true })}</div>`


export const pageStates = () => page({
  num: '07', title: 'States',
  artboards: [
    artboard('A. Needs credentials: sign-in prompt', 'Opened from the key icon or on first use. The password is write-only after this; the username stays readable. "Remember" is on for keychain connections; for "Ask each time" it starts off (D9).',
      shell({ active: '', tree: treeFull({ status: { prod_pg: 'creds' } }), main: homeStub() }) + '<div class="scrim"></div>' + needsCreds()),
    artboard('A3. Needs credentials: the server changed since you signed in', 'Credentials are tied to the host, port and database. After a pull changes them, Vibe asks again instead of sending your password to a new server.',
      shell({ active: '', tree: treeFull({ status: { prod_pg: 'creds' } }), main: homeStub() }) + '<div class="scrim"></div>' + fingerprint()),
    artboard('A2. Needs credentials: keychain unreachable', 'No plaintext-file fallback. Say what happened and offer the env: way.',
      shell({ active: '', tree: treeFull({ status: { prod_pg: 'creds' } }), main: homeStub() }) + '<div class="scrim"></div>' + keychainDown(), 'h-600'),
    artboard('B. Extension install consent', 'Never downloads without a yes. Names the extension, the source, where it goes and who needs it.',
      shell({ active: '', tree: treeFull(), main: homeStub() }) + '<div class="scrim"></div>' + extConsent()),
    artboard('C. Workspace opened read-only (hub held by vibe mcp serve)', 'A banner, not a dead end. Local files show the lock; remote connections keep working.',
      shell({ active: 'home', tree: roTree, banner: roBanner, main: homeStub(), topOpts: { pill: 'Agent can only read here', pillDot: 'warn' } })),
    artboard('C2. Take over', 'Explains who is affected and that nothing is deleted.',
      shell({ active: 'home', tree: roTree, banner: roBanner, main: homeStub(), topOpts: { pill: 'Agent can only read here', pillDot: 'warn' } }) + '<div class="scrim"></div>' + takeOver(), 'h-600'),
    artboard('D. "Changed on disk" conflict banner in a notebook', 'Sticky until resolved. Three real choices, none destructive by default (Reload is primary only because it loses the least in the common git-pull case; the dialog names what is lost).',
      shell({ active: 'notebooks', tree: treeFull(), main: conflictMain() })),
    artboard('E. Read-only datasheet shows the reason', 'Editing off, with why and how to change it. Same pattern if the connection is writable but the object is a view.',
      shell({ active: '', tree: treeFull({ sel: 'prod_pg' }), main: roSheetMain() })),
  ],
})

// ---- 08 query editor + cell -------------------------------------------
const connMenu = () => `
<div class="menu" role="listbox" aria-label="Run on" style="left:486px;top:120px;width:340px">
  <div class="mhead">Run this query on</div>
  <div class="mi active" role="option" aria-selected="true">${ctype('duckdb')}<span class="grow">local</span><span class="end">${scope('shared')}${st('ok')}${icon('check')}</span></div>
  <div class="mi" role="option">${ctype('postgres')}<span class="grow">prod_pg</span><span class="end">${icon('lock', 12)}${scope('shared')}${st('ok')}</span></div>
  <div class="mi" role="option">${ctype('postgres')}<span class="grow">warehouse</span><span class="end">${scope('local')}${st('creds')}</span></div>
  <div class="mi" role="option">${ctype('motherduck')}<span class="grow">md</span><span class="end">${scope('user')}${st('creds')}</span></div>
  <div class="mi" role="option">${ctype('sqlite')}<span class="grow">legacy</span><span class="end">${scope('local')}${st('off')}</span></div>
  <div class="mi disabled" role="option" aria-disabled="true">${ctype('duckdb')}<span class="grow">ops_db</span><span class="end">${scope('shared')}${st('lock')}</span></div>
  <div class="msep"></div>
  <div class="mnote">Plain table names run on the connection you pick. To use several at once, write <code>connection.schema.table</code>, like <code>prod_pg.public.customers</code>.</div>
  <div class="msep"></div>
  <div class="mi" role="menuitem">${icon('plug')}<span class="grow">Manage connections&hellip;</span></div>
</div>`

const queryMain = () => `
<div class="page-subhead" style="padding:12px 24px">
  <div><div class="crumb">Acme Analytics <span class="sep">/</span> Query Explorer</div><div class="row" style="gap:10px;margin-top:4px"><h1 style="font-size:22px;margin:0;font-family:var(--sh-font-mono)">top-customers.sql</h1></div></div>
  <span class="spacer"></span>
  <button class="conn-select" type="button" aria-haspopup="listbox" aria-expanded="true"><span class="lab">Run on</span>${ctype('duckdb')}local${icon('chevronDown', 14, 'class="cv"')}</button>
  <button class="btn btn-sm" type="button">${icon('save', 14)} Save query</button>
  <button class="btn btn-ghost btn-sm" type="button">${icon('sparkle', 14)} Ask agent</button>
  <button class="btn btn-primary btn-sm" type="button">${icon('play', 14)} Run &middot; Ctrl Enter</button>
</div>
<div style="flex:1;display:flex;min-height:0">
  <div class="col" style="width:210px;flex:none;border-right:2px solid var(--vibe-border);background:var(--sh-paper-2);padding:14px 12px;gap:6px">
    <div class="sh-label">Saved queries</div>
    ${['top-customers.sql', 'late-shipments.sql', 'revenue-by-month.sql', 'stock-below-reorder.sql'].map((q, i) => `<div style="padding:6px 8px;font-size:13px;font-weight:600;border-radius:2px;${i === 0 ? 'background:var(--sh-surface);border:1.5px solid var(--vibe-border);' : ''}">${q}</div>`).join('')}
  </div>
  <div style="flex:1;min-width:0;display:flex;flex-direction:column">
    <pre class="sqlbox" style="height:190px;border-bottom:2px solid var(--vibe-border);font-size:13px">-- Which prod customers spent the most this quarter?
<span class="k">SELECT</span> c.name,
       <span class="k">sum</span>(o.total) <span class="k">AS</span> spend
<span class="k">FROM</span>   <span class="q">local</span>.main.orders o
<span class="k">JOIN</span>   <span class="q">prod_pg</span>.public.customers c <span class="k">ON</span> c.id = o.customer_id
<span class="k">GROUP BY</span> 1 <span class="k">ORDER BY</span> 2 <span class="k">DESC LIMIT</span> 10;</pre>
    <div class="result-bar"><span class="badge badge--success"><span class="dot"></span>Complete</span><span><span class="big ok">10</span> rows</span><span><span class="big">0.84s</span></span><span>${icon('zap', 11)} DuckDB &middot; ran across local + prod_pg</span></div>
    <div class="scroll-pane">${datasheet({ cols: [{ n: 'name' }, { n: 'spend', num: true }], rows: [['Harbor Lane Hardware', '48,210.00'], ['Northwind Traders', '39,880.40'], ['Brightside Cafe', '31,402.95'], ['Maple & Main', '27,115.10'], ['Orchard Supply', '22,904.00']] })}</div>
  </div>
</div>`

const cellsMain = () => `
<div class="nb-wrap" style="overflow:auto">
  <div class="headrow" style="margin-bottom:0"><div><div class="crumb">Notebooks <span class="sep">/</span> weekly-revenue</div><h1>Weekly revenue</h1></div><button class="btn btn-sm" type="button">${icon('plus', 14)} Add cell</button></div>
  ${sqlCell({ k: 'SQL &middot; revenue by week', conn: 'local', code: `<span class="k">SELECT</span> date_trunc(<span class="s">'week'</span>, ordered_at) <span class="k">AS</span> week, <span class="k">sum</span>(total) <span class="k">AS</span> revenue\n<span class="k">FROM</span> orders <span class="k">GROUP BY</span> 1 <span class="k">ORDER BY</span> 1 <span class="k">DESC LIMIT</span> 12;`, result: `<div style="padding:10px 14px;font-size:12px;color:var(--sh-graphite);font-family:var(--sh-font-mono)">12 rows &middot; 0.03s &middot; no connection chosen, so this ran on the workspace default (local)</div>` })}
  ${sqlCell({ k: 'SQL &middot; open orders in production', conn: 'prod_pg', connType: 'postgres', explicit: true, code: `<span class="k">SELECT</span> status, <span class="k">count</span>(*) <span class="k">AS</span> n\n<span class="k">FROM</span> public.orders <span class="k">WHERE</span> status <span class="k">IN</span> (<span class="s">'pending'</span>, <span class="s">'packing'</span>) <span class="k">GROUP BY</span> 1;`, menu: `<div class="menu" role="listbox" aria-label="Run on" style="right:90px;top:42px;width:300px">
    <div class="mhead">Run this cell on</div>
    <div class="mi" role="option">${icon('history')}<span class="grow">Workspace default<span class="sub">local</span></span></div>
    <div class="mi" role="option">${ctype('duckdb')}<span class="grow">local</span><span class="end">${st('ok')}</span></div>
    <div class="mi active" role="option" aria-selected="true">${ctype('postgres')}<span class="grow">prod_pg</span><span class="end">${st('ok')}${icon('check')}</span></div>
    <div class="mi" role="option">${ctype('motherduck')}<span class="grow">md</span><span class="end">${st('creds')}</span></div>
    <div class="mnote">Saved with the cell, so a teammate runs it on the same connection.</div></div>`, result: `<div style="padding:0">${datasheet({ cols: [{ n: 'status' }, { n: 'n', num: true }], rows: [['pending', '412'], ['packing', '87']] })}</div>` })}
  ${sqlCell({ k: 'SQL &middot; spend by customer (local + prod_pg)', conn: 'local', code: `<span class="k">SELECT</span> c.name, <span class="k">sum</span>(o.total) <span class="k">FROM</span> <span class="q">local</span>.main.orders o <span class="k">JOIN</span> <span class="q">prod_pg</span>.public.customers c <span class="k">ON</span> c.id = o.customer_id <span class="k">GROUP BY</span> 1;`, result: `<div style="padding:10px 14px;font-size:12px;color:var(--sh-graphite)">Cells can join across connections by naming each table as <code>connection.schema.table</code>.</div>` })}
</div>`

export const pageQuery = () => page({
  num: '08', title: 'Query editor and SQL cells',
  artboards: [
    artboard('A. Query editor with the connection selector open', 'Replaces the editor\'s own schema tree; the global Connections rail does that now. The selector sets which connection plain table names mean.',
      shell({ active: 'queries', tree: treeFull(), main: queryMain(), menu: connMenu() })),
    artboard('B. Notebook SQL cells', 'Each cell can name its connection; unset means the workspace default and says so. Cell 3 joins across connections by qualifying names.',
      shell({ active: 'notebooks', tree: treeFull(), main: cellsMain() })),
  ],
})

// ---- 09 settings ------------------------------------------------------
const settingsHead = (tab) => `
<div class="headrow"><div><div class="crumb">Acme Analytics <span class="sep">/</span> Settings</div><h1>Settings</h1></div></div>
<div class="tabs" role="tablist" aria-label="Settings">
  <button role="tab" id="t-ws" aria-selected="${tab === 'ws'}" aria-controls="p-ws" type="button">Workspace<span class="tsub">Acme Analytics &middot; saved in this folder</span></button>
  <button role="tab" id="t-app" aria-selected="${tab === 'app'}" aria-controls="p-app" type="button">App<span class="tsub">This computer &middot; every workspace</span></button>
</div>`

const wsSettings = () => `
<div class="page-pad" style="overflow:auto;height:100%">
  ${settingsHead('ws')}
  <div class="col" style="max-width:820px;gap:28px" role="tabpanel" id="p-ws" aria-labelledby="t-ws">
    <div><div class="sec-title" style="margin-top:0"><h2>This workspace</h2><span class="label">vibe.yaml &middot; shared if you use git</span></div>
      <div class="card card-pad col" style="gap:16px">
        <div class="field"><label for="s-name">Name</label><input id="s-name" class="field-input" value="Acme Analytics"></div>
        <div class="field"><label>Folder</label><div class="row" style="gap:8px"><code style="flex:1;padding:8px 12px;display:block">~/work/acme-analytics</code><button class="btn btn-sm" type="button">${icon('externalLink', 14)} Show in folder</button></div></div>
        <div class="field"><label for="s-def">Default connection</label><select id="s-def" class="field-input" style="max-width:320px"><option>local</option><option>prod_pg</option><option>md</option></select><div class="help">Notebook cells and queries with no connection chosen run here. <code>local</code> is the safest default.</div></div>
        <div class="row"><button class="btn btn-primary" type="button">${icon('save', 16)} Save settings</button><span class="help">Changes are saved to <code>vibe.yaml</code>.</span></div>
      </div></div>
    <div><div class="sec-title" style="margin-top:0"><h2>The agent</h2><span class="label">Team limit in vibe.yaml &middot; your choices on this computer</span></div>
      <div class="card card-pad col" style="gap:18px">
        <div class="field"><label id="team-l">Team limit &middot; saved in vibe.yaml, shared through git</label>
          <div class="seg" role="radiogroup" aria-labelledby="team-l" style="align-self:flex-start"><button type="button" role="radio" aria-checked="false">Read only</button><button type="button" role="radio" aria-checked="true" class="on">Read and write</button><button type="button" role="radio" aria-checked="false">Full</button></div>
          <div class="help">What the agent may do for everyone on the team. This file can only make things <b>stricter</b>: a change someone pulls can never switch the agent's safeguards off on your computer.</div></div>
        <div class="field"><label id="mode-l">On this computer &middot; just you</label>
          <div class="seg" role="radiogroup" aria-labelledby="mode-l" style="align-self:flex-start"><button type="button" role="radio" aria-checked="false">Read only</button><button type="button" role="radio" aria-checked="true" class="on">Read and write</button><button type="button" role="radio" aria-checked="false" disabled>Full</button></div>
          <div class="help">Your own setting can be stricter than the team's, or looser where the team limit allows it. Full is greyed out because the team limit is Read and write. It is saved in <code>.vibe-local/</code>.</div></div>
        <div class="switch-row"><span class="toggle" role="switch" aria-checked="true" tabindex="0" aria-labelledby="ask-t" aria-describedby="ask-d"></span><span><span class="st-t" id="ask-t">Ask me before it changes data</span><span class="st-d" id="ask-d">Every change shows up as a card you approve or cancel. This switch only covers the workspace's own <code>local</code> database. For every other connection the agent always asks, and that can't be turned off. Saved on this computer only.</span></span></div>
        <table class="legend-table" style="border:1.5px solid var(--vibe-border);border-radius:2px"><thead><tr><th>Connection the agent can change</th><th>Asks first?</th></tr></thead><tbody>
          <tr><td class="k" style="font-weight:600">${ctype('duckdb')} local</td><td>Yes, your choice</td></tr>
          <tr><td class="k" style="font-weight:600">${ctype('postgres')} warehouse <span class="help">(read-only is off)</span></td><td>Always ${icon('lock', 12)}</td></tr>
          <tr><td class="k" style="font-weight:600">Everything else: prod_pg, md, staging_pg, legacy, lake</td><td class="help">Read-only. The agent can look but not change.</td></tr></tbody></table>
      </div></div>
    <div><div class="sec-title" style="margin-top:0"><h2>Claude and other tools</h2><span class="label">MCP</span></div>
      <div class="card card-pad col" style="gap:14px">
        <div class="row" style="gap:8px"><span class="status-dot ok"></span><span class="sh-label" style="margin:0">Running, only on this computer</span></div>
        <div class="field"><label>Address</label><div class="row" style="gap:8px"><code style="flex:1;padding:8px 12px;display:block">http://127.0.0.1:52817/sse</code><button class="btn btn-sm" type="button">${icon('copy', 14)} Copy</button></div></div>
        <div class="row" style="gap:10px;flex-wrap:wrap"><button class="btn" type="button">${icon('copy', 16)} Copy Claude Code config</button><button class="btn" type="button">${icon('copy', 16)} Copy Claude Desktop config</button></div>
        <div class="note warn">${icon('alert')}<span>With Studio closed, tools using <code>vibe mcp serve</code> can't change anything while "Ask me before it changes data" is on. Turn it off to let them change the workspace's own <code>local</code> database only; other connections always need Studio.</span></div>
      </div></div>
  </div>
</div>`

const swatches = (cols) => `<div class="sw">${cols.map((c) => `<i style="background:${c}"></i>`).join('')}</div>`

const appSettings = () => `
<div class="page-pad" style="overflow:auto;height:100%">
  ${settingsHead('app')}
  <div class="col" style="max-width:820px;gap:28px" role="tabpanel" id="p-app" aria-labelledby="t-app">
    <div><div class="sec-title" style="margin-top:0"><h2>Appearance</h2><span class="label">This computer</span></div>
      <div class="themes" role="radiogroup" aria-label="Theme">
        <button class="card themecard is-selected" type="button" role="radio" aria-checked="true">${swatches(['var(--vibe-bg)', 'var(--vibe-surface)', 'var(--vibe-accent)', 'var(--vibe-highlight)'])}<span class="tn2">SchoolHouse</span><span class="td2">The default</span></button>
        <button class="card themecard" type="button" role="radio" aria-checked="false">${swatches(['var(--vibe-fg)', 'var(--vibe-bg-alt)', 'var(--vibe-accent-hover)', 'var(--vibe-attention)'])}<span class="tn2">SchoolHouse Dark</span><span class="td2">Lights off</span></button>
        <button class="card themecard" type="button" role="radio" aria-checked="false">${swatches(['var(--vibe-highlight)', 'var(--vibe-surface)', 'var(--vibe-fg)', 'var(--vibe-chrome-bg)'])}<span class="tn2">Signal</span><span class="td2">High contrast</span></button>
        <button class="card themecard" type="button" role="radio" aria-checked="false">${swatches(['var(--vibe-bg-alt)', 'var(--vibe-info)', 'var(--vibe-chart-5)', 'var(--vibe-chart-6)'])}<span class="tn2">Your themes</span><span class="td2">From ~/.config/vibe/themes</span></button>
      </div></div>
    <div><div class="sec-title" style="margin-top:0"><h2>The agent's model</h2><span class="label">Used for every workspace</span></div>
      <div class="card card-pad col" style="gap:16px">
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px"><div class="field"><label for="l-p">Provider</label><select id="l-p" class="field-input"><option>Anthropic</option><option>Gemini</option><option>Local (OpenAI-compatible)</option></select></div><div class="field"><label for="l-m">Model</label><input id="l-m" class="field-input" value="claude-sonnet-4-5"></div></div>
        <div class="field"><label id="k-l">API key</label><div class="cred" role="group" aria-labelledby="k-l"><span class="dots" aria-label="Saved, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span><span class="where">${icon('shieldCheck')} In your keychain</span><span class="acts"><button class="linkbtn" type="button">Replace</button><span class="sepdot" aria-hidden="true">&middot;</span><button class="linkbtn danger" type="button">Clear</button></span></div>
          <div class="help">The <code>VIBE_LLM_API_KEY</code> environment variable, if set, is used instead.</div></div>
        <div class="note info">${icon('info')}<span>Found a key in <code>~/.vibe.yaml</code>? <button class="linkbtn" type="button" style="padding:0 4px">Move it to the keychain</button> and blank the file.</span></div>
      </div></div>
    <div><div class="sec-title" style="margin-top:0"><h2>Extensions</h2><span class="label">Add-ons the engine downloads once</span></div>
      <div class="card card-pad"><div class="ext-row" style="padding-top:0"><span class="en">postgres</span><span>Postgres connections &middot; needed by <code>prod_pg</code>, <code>warehouse</code>, <code>staging_pg</code></span><span class="badge badge--success"><span class="dot"></span>Installed</span><span><button class="linkbtn danger" type="button">Remove</button></span></div>
        <div class="ext-row"><span class="en">httpfs</span><span>S3, GCS and R2 files &middot; needed by <code>lake</code></span><span class="badge badge--info"><span class="dot"></span>Not installed</span><span><button class="btn btn-sm" type="button">${icon('download', 14)} Install</button></span></div>
        <div class="ext-row"><span class="en">sqlite</span><span>SQLite files &middot; needed by <code>legacy</code></span><span class="badge badge--success"><span class="dot"></span>Installed</span><span><button class="linkbtn danger" type="button">Remove</button></span></div>
        <div class="ext-row" style="padding-bottom:0"><span class="en">motherduck</span><span>MotherDuck &middot; needed by <code>md</code></span><span class="badge badge--success"><span class="dot"></span>Installed</span><span><button class="linkbtn danger" type="button">Remove</button></span></div>
        <p class="help" style="margin-top:14px">Stored in Vibe's own folder, separate from any DuckDB you installed yourself.</p></div></div>
  </div>
</div>`

export const pageSettings = () => page({
  num: '09', title: 'Settings split',
  artboards: [
    artboard('A. Workspace tab', 'Things that belong to the folder and travel with git. Ask-before-changes is forced on for non-local connections, and the table says why.',
      shell({ active: 'settings', tree: treeFull(), main: wsSettings() }), 'h-1500'),
    artboard('B. App tab', 'Things that belong to this computer. The LLM key moves to the keychain; extensions get a status list.',
      shell({ active: 'settings', tree: treeFull(), main: appSettings() }), 'h-1200'),
  ],
})

// ---- 10 agent approval ---------------------------------------------------
const approval = ({ title, conn, connType, scopeName, summary, sql, state = '', destructive = false, note }) => `
<div class="approval" role="group" aria-label="Approval needed">
  <div class="ap-head">${icon('shield', 18, 'style="color:var(--sh-coral)"')}<span class="t">${title}</span></div>
  <div class="ap-body">
    <span class="ap-conn">${ctype(connType)}${conn}<span class="small">not your local data</span></span>
    <p>${summary}</p>
    <pre>${sql}</pre>
    ${note ? `<p class="help">${note}</p>` : ''}
  </div>
  <div class="ap-foot"><button class="btn btn-sm" type="button" ${state ? 'disabled' : ''}>Cancel</button><button class="btn btn-sm ${destructive ? 'btn-destructive' : 'btn-primary'}" type="button" ${state ? 'disabled' : ''}>${destructive ? 'Delete rows in ' + conn : 'Allow change to ' + conn}</button></div>
  <div class="ap-state" role="status" aria-live="polite">${state}</div>
</div>`

const agentPanel = (stream) => `
<aside class="agent-side agent-over" aria-label="Agent">
  <div class="head"><span class="title">${icon('bot', 18)} Agent</span><span class="meta"><span class="status-dot ok"></span>Reads freely &middot; asks first</span></div>
  <div class="stream" style="overflow:auto">${stream}</div>
  <div class="input"><div class="tools"><span class="ic">${icon('plug', 11)} Default: local</span><span class="ic">${icon('shield', 11)} Asks before changes</span></div><div class="box">Ask about your data&hellip;</div></div>
</aside>`

const msgUser = (t) => `<div class="amsg"><span class="av user">ZO</span><div class="body"><div class="who">You</div><div class="user-bubble">${t}</div></div></div>`
const msgBot = (t, extra = '') => `<div class="amsg"><span class="av bot">V</span><div class="body"><div class="who">Agent</div><p>${t}</p>${extra}</div></div>`
const chip = (l, r = '<span class="check">&#10003;</span>') => `<div class="tool-chip"><span class="lab">${icon('terminal', 12)} ${l}</span>${r}</div>`

const nbBehind = () => `<div class="nb-wrap"><div class="headrow" style="margin-bottom:0"><div><div class="crumb">Notebooks <span class="sep">/</span> weekly-revenue</div><h1>Weekly revenue</h1></div></div>
  ${sqlCell({ k: 'SQL &middot; revenue by week', conn: 'local', code: `<span class="k">SELECT</span> date_trunc(<span class="s">'week'</span>, ordered_at) week, <span class="k">sum</span>(total) revenue <span class="k">FROM</span> orders <span class="k">GROUP BY</span> 1;` })}</div>`

export const pageAgent = () => page({
  num: '10', title: 'Agent approval card',
  artboards: [
    artboard('A. Write to a non-local connection', 'The card names the connection, its scope and the exact statement. "Allow" is the only way it runs. The agent can already read; this is about changing.',
      shell({ active: 'notebooks', tree: treeFull(), main: nbBehind(), overlay: agentPanel(
        msgUser('Mark order 10482 as shipped in the warehouse database.') +
        msgBot("I'll check the order first, then update it.", chip('describe_table warehouse.public.orders') + chip('preview_query &middot; 1 row') ) +
        approval({ title: 'Allow this change to warehouse?', conn: 'warehouse', connType: 'postgres', scopeName: 'local', summary: 'Update 1 row in <b>warehouse</b>: set the status of order 10482 to "shipped".', sql: "UPDATE warehouse.public.orders\nSET status = 'shipped'\nWHERE id = 10482;", note: "This is outside your workspace's local data, so Vibe can't undo it for you." }) +
        msgUser('Now do the same in prod.') +
        msgBot("I can't. <b>prod_pg</b> is set to read-only, so I can look but not change it. If you want me to, turn off Read-only on that connection first.") ) }), 'h-1000'),
    artboard('B. Other states of the same card', 'Destructive wording is explicit; approved shows progress; the headless MCP case says why it is blocked.',
      `<div class="center-stage" style="align-items:flex-start;overflow:auto"><div style="display:grid;grid-template-columns:repeat(3,1fr);gap:20px;width:100%;max-width:1200px">
        <div class="col" style="gap:8px"><div class="sh-label">Destructive, pending</div>${approval({ title: 'Delete rows from warehouse?', conn: 'warehouse', connType: 'postgres', scopeName: 'local', destructive: true, summary: 'Delete <b>1,204 rows</b> from <b>warehouse</b>.public.events where the row is older than 2025-01-01.', sql: "DELETE FROM warehouse.public.events\nWHERE created_at < '2025-01-01';", note: 'This can\'t be undone. Check the number of rows before you allow it.' })}</div>
        <div class="col" style="gap:8px"><div class="sh-label">Approved, running</div>${approval({ title: 'Allow this change to warehouse?', conn: 'warehouse', connType: 'postgres', scopeName: 'local', summary: 'Update 1 row in <b>warehouse</b>: set the status of order 10482 to "shipped".', sql: "UPDATE warehouse.public.orders\nSET status = 'shipped'\nWHERE id = 10482;", state: 'Approved, running…' })}</div>
        <div class="col" style="gap:8px"><div class="sh-label">Asked through vibe mcp serve, Studio closed</div><div class="banner attn" role="status" style="align-items:flex-start"><div class="ico">${icon('lock')}</div><div class="body"><div class="ttl">Blocked: nobody to ask</div><div class="sub" style="color:var(--sh-ink)">A tool tried to change <b>prod_pg</b> through <code>vibe mcp serve</code>, but this workspace isn't open in Vibe Studio, so there's no one to approve it. Open the workspace in Vibe Studio to allow it, or let the tool read only.</div></div></div></div>
      </div></div>`, 'h-600'),
  ],
})
