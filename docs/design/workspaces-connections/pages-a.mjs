// Screens 01-04: launcher, new workspace, import, switcher.
import { icon, page, artboard, shell, TREE_FULL, treeFull, ctype, scope, st } from './lib.mjs'

const recent = ({ nm, path, branch, when, sel, missing }) => `
<li class="recent-li${missing ? ' missing' : ''}">
  <button class="recent${sel ? ' sel kf' : ''}${missing ? ' missing' : ''}" type="button"${missing ? ' aria-describedby="missing-help"' : ''}>
    <span class="fico">${icon(missing ? 'alert' : 'folderOpen')}</span>
    <span style="min-width:0"><span class="nm">${nm}</span><span class="path">${path}</span></span>
    <span class="tail">${missing
      ? `<span class="badge badge--warn"><span class="dot"></span>Folder not found</span>`
      : `${branch ? `<span class="branch">${icon('gitBranch', 12)}${branch}</span>` : `<span class="branch" style="color:var(--sh-graphite)">no git</span>`}<span>${when}</span>`}</span>
  </button>
  ${missing ? `<button class="linkbtn" type="button">Locate&hellip;</button>` : ''}
  <button class="icon-btn" type="button" aria-label="Remove ${nm} from the list" title="Remove from list (doesn't delete anything)">${icon('x')}</button>
</li>`

export const launcherRecents = () => `
<div class="launcher"><div class="lw">
  <div class="brandrow"><span class="markbig" aria-hidden="true">V</span><h1>Vibe <span>Studio</span></h1></div>
  <p class="lead">Open a folder to work on your data. Notebooks, saved queries and skills live in that folder as plain files.</p>
  <div class="cols">
    <section aria-labelledby="rh">
      <div class="sec-title" style="margin-top:0"><h2 id="rh">Recent workspaces</h2><span class="label">Last opened first</span></div>
      <div class="card" style="overflow:hidden">
        <ul role="list" style="list-style:none;margin:0;padding:0">
          ${recent({ nm: 'Acme Analytics', path: '~/work/acme-analytics', branch: 'main', when: '2 hours ago', sel: true })}
          ${recent({ nm: 'Harbor Lane Hardware', path: '~/Documents/harbor-lane', branch: null, when: 'Yesterday' })}
          ${recent({ nm: 'Northwind Client', path: '~/clients/northwind', branch: 'reporting', when: '3 days ago' })}
          ${recent({ nm: 'Old Ledger', path: '/Volumes/Backup/ledger-2019', missing: true })}
        </ul>
      </div>
      <p id="missing-help" class="help" style="margin-top:10px">Old Ledger's folder isn't there anymore. It may be on a drive that isn't plugged in. Locate it, or remove it from this list. Removing never deletes files.</p>
    </section>
    <section aria-label="Start something">
      <div class="col" style="gap:14px">
        <button class="card act-card primary" type="button"><span class="ai">${icon('folderOpen', 20)}</span><span><span class="at">Open folder&hellip; <kbd>Ctrl O</kbd></span><span class="ad">Choose a workspace, or any folder you'd like to turn into one.</span></span></button>
        <button class="card act-card" type="button"><span class="ai">${icon('plus', 20)}</span><span><span class="at">New workspace&hellip; <kbd>Ctrl N</kbd></span><span class="ad">Make a new folder. Pick a name, turn on git if you like, start blank or from a template.</span></span></button>
        <button class="card act-card" type="button"><span class="ai">${icon('download', 20)}</span><span><span class="at">Import a .vibe file&hellip;</span><span class="ad">Bring an older Vibe file, or a bundle someone shared, into a new workspace.</span></span></button>
      </div>
    </section>
  </div>
  <p class="trust-line">${icon('shieldCheck')}<span>Your data stays where it already lives. Vibe keeps passwords in this computer's keychain, never in the workspace folder, so a workspace is safe to put in git or share with a teammate.</span></p>
</div></div>`

export const launcherFirstRun = () => `
<div class="launcher"><div class="lw" style="display:flex;flex-direction:column;align-items:center">
  <div class="brandrow"><span class="markbig" aria-hidden="true">V</span><h1>Vibe <span>Studio</span></h1></div>
  <div class="empty-hero" style="padding-top:36px">
    <span class="glyph">${icon('folderOpen')}</span>
    <h2>Pick a folder to start</h2>
    <p class="lead">A workspace is just a folder. Your notebooks, saved queries and skills are kept in it as ordinary files, so you can back it up or share it with git. Your data stays wherever it is now.</p>
    <div class="row" style="gap:14px;margin-top:6px">
      <button class="btn btn-primary" type="button">${icon('plus')} New workspace&hellip;</button>
      <button class="btn" type="button">${icon('folderOpen')} Open folder&hellip;</button>
    </div>
    <p class="help">Have an older .vibe file? <button class="linkbtn" type="button" style="padding:0 2px">Import it</button> and Vibe will set up a workspace for you.</p>
  </div>
  <div class="hero-mini">
    <div class="card"><b>Notebooks</b><span>Questions about your data that you can re-run any time. Answers are never frozen.</span></div>
    <div class="card"><b>Connections</b><span>Point Vibe at Postgres, MotherDuck, SQLite files or cloud storage. Add them whenever you're ready.</span></div>
    <div class="card"><b>The agent</b><span>Ask in plain words. It reads freely and asks you before it changes anything.</span></div>
  </div>
  <p class="trust-line" style="margin-top:32px">${icon('shieldCheck')}<span>Passwords and tokens go in this computer's keychain, not in your folder.</span></p>
</div></div>`

const field = (label, id, inner, help = '') => `<div class="field"><label for="${id}">${label}</label>${inner}${help ? `<div class="help">${help}</div>` : ''}</div>`

const tmplCard = (t, d, on) => `<label class="opt-card${on ? ' on' : ''}" style="cursor:pointer"><input type="radio" name="tmpl" ${on ? 'checked' : ''} aria-label="${t}"><span><span class="ot">${t}</span><span class="od">${d}</span></span></label>`

const newWorkspaceDialog = ({ error = false } = {}) => `
<div class="modal-overlay" role="presentation">
  <div class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="nw-h">
    <div class="d-head"><h2 id="nw-h">New workspace</h2><p>A workspace is a folder. Vibe makes it for you and puts everything inside.</p></div>
    <div class="d-body">
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
        ${field('Name', 'nw-name', `<input id="nw-name" class="field-input" value="${error ? 'Acme Analytics' : 'Acme Analytics'}" aria-describedby="nw-name-h">`, 'Shown in the window title and the workspace menu.')}
        ${field('Where to put it', 'nw-loc', `<div class="row" style="gap:8px"><input id="nw-loc" class="field-input${error ? ' field-input-error' : ''}" value="~/work" ${error ? 'aria-invalid="true" aria-describedby="nw-err"' : ''}><button class="btn" type="button" style="flex:none">Choose&hellip;</button></div>`, error ? '' : 'Vibe creates a new folder here.')}
      </div>
      ${error
        ? `<div class="note attn" id="nw-err" role="alert">${icon('alert')}<span><b>There's already a folder called acme-analytics in ~/work.</b> Pick another name or location. Vibe won't put a workspace inside a folder that already has files in it, unless you use Open folder instead.</span></div>`
        : `<div class="url-tile" style="font-size:13px" aria-label="The folder that will be created"><span class="sh-label" style="display:block;margin-bottom:4px">Will create</span>~/work/<span class="acc">acme-analytics</span></div>`}
      <div class="switch-row"><input type="checkbox" id="nw-git" checked><label for="nw-git"><span class="st-t">Start a git repository</span><span class="st-d">So you can track changes and share notebooks with your team. Vibe never commits or pushes for you.</span></label></div>
      <fieldset style="border:0;margin:0;padding:0"><legend class="field-label" style="padding:0">Start from</legend>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
          ${tmplCard('Blank', 'An empty local database. Import a CSV or connect something later.', true)}
          ${tmplCard('Contacts', 'People and their notes. A small table to try things on.')}
          ${tmplCard('Inventory', 'Suppliers and products, with stock levels.')}
          ${tmplCard('Order entry', 'Customers, orders and order lines, with a starter notebook.')}
          ${tmplCard('Training registrations', 'Sessions and sign-ups.')}
        </div>
      </fieldset>
      <div class="note info">${icon('info')}<span>This creates <code>vibe.yaml</code>, <code>notebooks/</code>, <code>queries/</code>, <code>skills/</code> and <code>data/local.duckdb</code>. The database file is kept out of git unless you choose otherwise.</span></div>
    </div>
    <div class="d-foot"><span class="left">${error ? '' : 'You can change all of this later in Settings.'}</span><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button" ${error ? 'disabled' : ''}>Create workspace</button></div>
  </div>
</div>`

const makeWsDialog = () => `
<div class="modal-overlay" role="presentation">
  <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="mk-h" aria-describedby="mk-d">
    <div class="d-head"><h2 id="mk-h">Make this folder a workspace?</h2></div>
    <div class="d-body" id="mk-d">
      <div class="url-tile" style="font-size:13px">~/Documents/<span class="acc">q3-reports</span></div>
      <p>This folder doesn't have a <code>vibe.yaml</code> yet. Vibe will add one, plus <code>notebooks/</code>, <code>queries/</code>, <code>skills/</code> and <code>data/</code>. Your other files are left alone and nothing is moved.</p>
    </div>
    <div class="d-foot"><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Make it a workspace</button></div>
  </div>
</div>`

export const pageLauncher = () => page({
  num: '01', title: 'Launcher',
  artboards: [
    artboard('A. Recent workspaces', 'Replaces UnlockView. Row = open. Arrow keys move, Enter opens, Delete removes from list. Missing folder keeps its row.', launcherRecents()),
    artboard('B. First run', 'No recents yet: one clear next step, plain words, trust line.', launcherFirstRun(), 'h-900'),
    artboard('C. Open folder on a plain folder', 'Folder has no vibe.yaml: ask before writing anything.', `<div style="position:absolute;inset:0;opacity:1">${launcherRecents()}</div><div class="scrim" aria-hidden="true"></div>${makeWsDialog()}`, 'h-600'),
  ],
})

export const pageNew = () => page({
  num: '02', title: 'New workspace',
  artboards: [
    artboard('A. Default', 'Dialog over the launcher (also opened from the switcher menu).', `<div style="position:absolute;inset:0">${launcherRecents()}</div><div class="scrim"></div>${newWorkspaceDialog()}`, 'h-900'),
    artboard('B. Folder already exists', 'Inline error, Create disabled until fixed. Focus returns to the Where field.', `<div style="position:absolute;inset:0">${launcherRecents()}</div><div class="scrim"></div>${newWorkspaceDialog({ error: true })}`, 'h-900'),
  ],
})

// ---- import ----
const stepper = (n) => `<div class="stepper" aria-label="Step ${n} of 3">
  <span class="${n > 1 ? 'done' : 'on'}"><span class="n">${n > 1 ? icon('check', 12) : 1}</span> Unlock</span><span class="bar"></span>
  <span class="${n > 2 ? 'done' : n === 2 ? 'on' : ''}"><span class="n">${n > 2 ? icon('check', 12) : 2}</span> Preview</span><span class="bar"></span>
  <span class="${n === 3 ? 'on' : ''}"><span class="n">3</span> Done</span></div>`

const importFrame = (inner) => `<div class="center-stage">${inner}</div>`

const impUnlock = ({ wrong = false } = {}) => importFrame(`
<div class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="im1">
  <div class="d-head">${stepper(1)}<h2 id="im1" style="margin-top:12px">Import a .vibe file</h2><p>Older Vibe files keep their data inside one locked file. Vibe will copy it into a normal workspace folder. The original isn't changed.</p></div>
  <div class="d-body">
    <div class="url-tile" style="font-size:13px;display:flex;gap:10px;align-items:center">${icon('lock', 16)}<span style="flex:1">~/Documents/<span class="acc">harbor-lane.vibe</span></span><button class="linkbtn" type="button">Choose another&hellip;</button></div>
    <div class="field"><label for="pp">Passphrase</label>
      <input id="pp" class="field-input${wrong ? ' field-input-error' : ''}" type="password" value="${wrong ? 'harborlane1' : ''}" placeholder="The passphrase you chose when you made this file" ${wrong ? 'aria-invalid="true" aria-describedby="pp-err"' : 'aria-describedby="pp-h"'} autocomplete="off">
      ${wrong ? `<div class="help" id="pp-err" style="color:var(--sh-error);font-weight:600">That passphrase didn't work. Check Caps Lock and try again.</div>` : ''}
    </div>
    <div class="note attn" id="pp-h">${icon('alert')}<span><b>Vibe can't recover a forgotten passphrase.</b> There is no reset and no back door: the passphrase is the only key. If it's lost, nobody can open this file, including us.</span></div>
    ${wrong ? `<details class="help"><summary style="cursor:pointer;font-weight:600">Show technical details</summary><div class="details" style="margin-top:8px">decrypt failed: HMAC check failed (file is intact, key is wrong)</div></details>` : ''}
  </div>
  <div class="d-foot"><span class="left">Got a bundle, not an old file? It opens straight to the preview.</span><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">Unlock and preview</button></div>
</div>`)

const impPreview = () => importFrame(`
<div class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="im2">
  <div class="d-head">${stepper(2)}<h2 id="im2" style="margin-top:12px">Here's what Vibe will create</h2><p>Nothing is written until you press Import.</p></div>
  <div class="d-body">
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
      <div class="field"><label for="iw-name">Workspace name</label><input id="iw-name" class="field-input" value="Harbor Lane Hardware"></div>
      <div class="field"><label for="iw-loc">Where to put it</label><div class="row" style="gap:8px"><input id="iw-loc" class="field-input" value="~/work"><button class="btn" type="button" style="flex:none">Choose&hellip;</button></div></div>
    </div>
    <div class="count-row" role="list" aria-label="What was found in the file">
      <div class="stat" role="listitem"><div class="label">Notebooks</div><div class="num">6</div></div>
      <div class="stat" role="listitem"><div class="label">Saved queries</div><div class="num">4</div></div>
      <div class="stat" role="listitem"><div class="label">Skills</div><div class="num">2</div></div>
      <div class="stat" role="listitem"><div class="label">Tables</div><div class="num">9</div></div>
    </div>
    <div class="tree-plan" role="group" aria-label="Files and folders that will be created">
      <span class="new">~/work/harbor-lane-hardware/</span>
      <span class="ind1"><span class="new">vibe.yaml</span> <span class="d">&nbsp;two connections: harbor_lane (your data, the default) and local (empty)</span></span>
      <span class="ind1">notebooks/ <span class="d">&nbsp;6 notebooks (2 older pages become notebooks)</span></span>
      <span class="ind1">queries/ <span class="d">&nbsp;4 saved queries</span></span>
      <span class="ind1">skills/ <span class="d">&nbsp;2 skills</span></span>
      <span class="ind1">data/</span>
      <span class="ind2"><span class="new">harbor-lane.duckdb</span> <span class="d">&nbsp;your 9 tables, 797 orders, 14 MB. Kept out of git.</span></span>
      <span class="ind2"><span class="new">local.duckdb</span> <span class="d">&nbsp;an empty database for new work. Kept out of git.</span></span>
    </div>
    <div class="note warn">${icon('lock')}<span><b>In the new workspace your data is a normal file, not locked with a passphrase.</b> Use your computer's disk encryption if this data is sensitive.</span></div>
    <div class="note info">${icon('info')}<span>Your original <code>harbor-lane.vibe</code> stays exactly as it is. Delete it once you've checked the new workspace.</span></div>
  </div>
  <div class="d-foot"><button class="btn" type="button">Back</button><button class="btn" type="button">Cancel</button><button class="btn btn-primary" type="button">${icon('download')} Import</button></div>
</div>`)

const impResult = () => importFrame(`
<div class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="im3">
  <div class="d-head">${stepper(3)}<h2 id="im3" style="margin-top:12px">Imported into a new workspace</h2></div>
  <div class="d-body">
    <div class="banner teal" role="status"><div class="ico">${icon('check')}</div><div class="body"><div class="ttl">Harbor Lane Hardware is ready</div><div class="sub" style="font-family:var(--sh-font-mono)">~/work/harbor-lane-hardware</div></div></div>
    <ul role="list" style="list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px;font-size:14px">
      <li class="row" style="gap:10px">${icon('circleCheck')}<span>6 notebooks, 4 saved queries and 2 skills were written as files.</span></li>
      <li class="row" style="gap:10px">${icon('circleCheck')}<span>Your data was copied to <code>data/harbor-lane.duckdb</code> and added as the connection <code>harbor_lane</code>, the default. A new empty <code>local</code> database is there too.</span></li>
      <li class="row" style="gap:10px;align-items:flex-start">${icon('alert', 16, 'style="color:var(--sh-warn);flex:none;margin-top:3px"')}<span>2 older pages were converted to notebooks. Charts and tables carry over, but have a quick look at <b>Monthly summary</b> and <b>Reorder list</b>. <button class="linkbtn" type="button" style="padding:0 2px">See what changed</button></span></li>
    </ul>
  </div>
  <div class="d-foot"><span class="left">${icon('lock', 12)} The old file's passphrase is no longer needed.</span><button class="btn" type="button">Show in folder</button><button class="btn btn-primary" type="button">Open workspace</button></div>
</div>`)

export const pageImport = () => page({
  num: '03', title: 'Import .vibe',
  artboards: [
    artboard('A. Unlock (older, locked file)', 'Plain language, the "can\'t be recovered" warning up front, no jargon on a wrong passphrase.', impUnlock(), 'h-600'),
    artboard('A2. Wrong passphrase', 'Inline error on the field, technical details tucked away.', impUnlock({ wrong: true }), 'h-600'),
    artboard('B. Preview', 'What will be created, as a tree. Nothing written yet. (Bundles skip step 1 when not encrypted.)', impPreview(), 'h-900'),
    artboard('C. Result', 'Success, what was written, and anything to double-check.', impResult(), 'h-600'),
  ],
})

// ---- switcher ----
const homeMain = () => `
<div class="page-pad" style="overflow:auto">
  <div class="headrow"><div><div class="crumb">Acme Analytics <span class="sep">/</span> Home</div><h1>Welcome back</h1></div></div>
  <div class="stats" style="grid-template-columns:repeat(3,1fr)">
    <div class="stat"><div class="label">Connections</div><div class="num">8 <small>2 connected</small></div><div class="delta">Others connect when you first use them</div></div>
    <div class="stat"><div class="label">Notebooks</div><div class="num">12</div><div class="delta">Last edited 2 hours ago</div></div>
    <div class="stat"><div class="label">Saved queries</div><div class="num">9</div><div class="delta">3 use prod_pg</div></div>
  </div>
  <div class="sec-title"><h2>Your tables</h2><span class="label">local &middot; the workspace's own database</span></div>
  <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:8px">
    ${[['customers', '12,480'], ['orders', '48,213'], ['products', '1,206'], ['v_monthly_revenue', 'view']].map(([n, c]) => `<div class="card card-pad" style="padding:12px 14px;box-shadow:var(--sh-shadow-sm)"><div class="row" style="gap:8px">${icon('tableIcon', 14)}<b style="font-family:var(--sh-font-mono);font-size:13px">${n}</b></div><div class="help">${c}${c === 'view' ? '' : ' rows'}</div></div>`).join('')}
  </div>
  <div class="sec-title"><h2>Jump back in</h2><span class="label">Recent</span></div>
  <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:16px">
    ${['Weekly revenue', 'Churn watchlist', 'Inventory check'].map((n, i) => `<div class="card card-pad"><div class="sh-label">Notebook</div><div style="font-family:var(--sh-font-display);font-size:20px;font-weight:600;margin:6px 0">${n}</div><div class="help">${['Edited 2 hours ago', 'Edited yesterday', 'Edited 3 days ago'][i]}</div></div>`).join('')}
  </div>
</div>`

const switcherMenu = () => `
<div class="menu" role="menu" aria-label="Workspaces" style="left:64px;top:56px;width:400px">
  <div class="mhead">Current workspace</div>
  <div class="mi active" role="menuitemradio" aria-checked="true">${icon('folderOpen')}<span class="grow"><span class="t">Acme Analytics</span><span class="sub">~/work/acme-analytics &middot; main</span></span><span class="end">${icon('check')}</span></div>
  <div class="mhead">Recent</div>
  <div class="mi kf" role="menuitemradio" aria-checked="false">${icon('folderOpen')}<span class="grow"><span class="t">Harbor Lane Hardware</span><span class="sub">~/Documents/harbor-lane &middot; no git</span></span><span class="end">Yesterday</span></div>
  <div class="mi" role="menuitemradio" aria-checked="false">${icon('folderOpen')}<span class="grow"><span class="t">Northwind Client</span><span class="sub">~/clients/northwind &middot; reporting</span></span><span class="end"><span class="badge badge--info" style="padding:1px 8px;font-size:11px" title="Uses a different MotherDuck login. Vibe will restart to switch.">${icon('refresh', 12)} Restarts</span></span></div>
  <div class="mi disabled" role="menuitemradio" aria-checked="false" aria-disabled="true">${icon('alert')}<span class="grow"><span class="t">Old Ledger</span><span class="sub">Folder not found</span></span><span class="end"><button class="linkbtn" type="button">Locate&hellip;</button></span></div>
  <div class="msep"></div>
  <div class="mi" role="menuitem">${icon('folderOpen')}<span class="grow"><span class="t">Open folder&hellip;</span></span><span class="end"><kbd>Ctrl O</kbd></span></div>
  <div class="mi" role="menuitem">${icon('plus')}<span class="grow"><span class="t">New workspace&hellip;</span></span><span class="end"><kbd>Ctrl N</kbd></span></div>
  <div class="mi" role="menuitem">${icon('download')}<span class="grow"><span class="t">Import a .vibe file&hellip;</span></span></div>
  <div class="msep"></div>
  <div class="mi" role="menuitem">${icon('layers')}<span class="grow"><span class="t">Manage workspaces&hellip;</span><span class="sub">Opens the launcher</span></span></div>
  <div class="mnote">Switching saves your open notebooks first. Nothing you were working on is lost.</div>
</div>`

const relaunchDialog = () => `
<div class="modal-overlay" role="presentation">
  <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="rl-h" aria-describedby="rl-d">
    <div class="d-head"><h2 id="rl-h">Restart to open Northwind Client?</h2></div>
    <div class="d-body" id="rl-d">
      <p>Northwind Client signs in to MotherDuck with a different login than Acme Analytics. Vibe can only use one MotherDuck login at a time, so it needs to restart to switch.</p>
      <div class="note info">${icon('info')}<span>Your notebooks are saved first, and Vibe will reopen on Northwind Client by itself.</span></div>
    </div>
    <div class="d-foot"><button class="btn" type="button">Stay in Acme Analytics</button><button class="btn btn-primary" type="button">${icon('refresh')} Restart and switch</button></div>
  </div>
</div>`

export const pageSwitcher = () => page({
  num: '04', title: 'Shell with workspace switcher',
  artboards: [
    artboard('A. Switcher open', 'Folder name + git branch in the top bar. Ctrl/Cmd+Shift+O opens it. Up/Down, Enter to switch, Esc closes and returns focus to the button.',
      shell({ active: 'home', tree: treeFull(), main: homeMain(), topOpts: { switcherOpen: true }, menu: switcherMenu() })),
    artboard('B. Switching to a workspace with a different MotherDuck login (D10)', 'The only switch that restarts the app. Plain words, one choice that is safe.',
      shell({ active: 'home', tree: treeFull(), main: homeMain() }) + `<div class="scrim"></div>` + relaunchDialog()),
  ],
})
