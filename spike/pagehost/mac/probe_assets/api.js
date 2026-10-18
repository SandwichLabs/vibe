(() => {
  const pend = new Map(); let n = 0;
  const post = (m) => window.webkit.messageHandlers.vibe.postMessage(JSON.stringify(m));
  Object.defineProperty(window, '__vibe_reply', { value: (id, r) => { const p = pend.get(id); if (p) { pend.delete(id); p(r); } } });
  Object.defineProperty(window, 'vibe', { value: Object.freeze({
    run: (name, args) => new Promise((res) => { const id = ++n; pend.set(id, res); post({ t: 'run', id, name, args }); }),
    params: () => ({}), use: async () => null }) });
  Object.defineProperty(window, '__spike', { value: Object.freeze({ log: (o) => post({ t: 'log', o }) }) });
  setInterval(() => post({ t: 'hb', i: window.__inline, p: window.__p, rs: document.readyState }), 250);
  window.addEventListener('error', (e) => post({ t: 'log', o: { v: 'js-error', r: e.message + ' @' + e.filename + ':' + e.lineno } }));
  window.addEventListener('unhandledrejection', (e) => post({ t: 'log', o: { v: 'js-unhandled', r: String(e.reason) } }));
  document.addEventListener('securitypolicyviolation', (e) => post({ t: 'log', o: { v: 'csp-violation', r: e.violatedDirective + ' ' + e.blockedURI } }));
})();