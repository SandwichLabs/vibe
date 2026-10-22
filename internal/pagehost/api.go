package pagehost

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// APIScript is the page-side API the helper injects as a user script in the
// main world, before the page runs (docs/plans/html-artifacts.md, H4):
//
//	vibe.run(query, values)   Promise of {columns, rows, truncated}
//	vibe.params()             the declared parameters, defaults and options
//	vibe.use(name)            Promise of a capability object, or null
//
// It also posts a heartbeat every 500 ms, which is what Studio's watchdog
// listens for. The page is untrusted and may replace anything it can reach, so
// the script keeps its own references to what it calls and defines its two
// globals as non-writable, non-configurable. A page that breaks the script
// breaks only itself (and its heartbeat, which stops the window).
//
// paramsJSON is a JSON object (the manifest's parameters); caps are the
// capabilities Studio grants, none until the capability steps are built.
// Both reach the script as string literals handed to JSON.parse, never as
// script text.
func APIScript(paramsJSON []byte, caps []string) string {
	sorted := append([]string(nil), caps...)
	sort.Strings(sorted)
	capsJSON, _ := json.Marshal(sorted)
	errs := map[string]string{}
	for k, v := range ErrorText {
		errs[k] = v
	}
	errJSON, _ := json.Marshal(errs)
	r := strings.NewReplacer("@ERRORS@", jsLiteral(errJSON), "@PARAMS@", jsLiteral(paramsJSON), "@CAPS@", jsLiteral(capsJSON))
	return r.Replace(apiTemplate)
}

// jsLiteral is a JavaScript string literal whose value is b.
func jsLiteral(b []byte) string {
	lit, err := json.Marshal(string(b))
	if err != nil {
		return `"{}"`
	}
	return string(lit)
}

// ReplyScript is the script the helper evaluates to answer a pending request
// from the page: the id, and either the result (JSON, handed over as a string
// literal so a key such as "__proto__" stays data) or an error code.
func ReplyScript(id int64, ok bool, result []byte, code string) string {
	if ok {
		return fmt.Sprintf("window.__vibeReply(%d,true,JSON.parse(%s))", id, jsLiteral(result))
	}
	return fmt.Sprintf("window.__vibeReply(%d,false,%s)", id, jsLiteral([]byte(code)))
}

const apiTemplate = `(function () {
  'use strict';
  var handler = window.webkit.messageHandlers.vibe;
  var post = handler.postMessage.bind(handler);
  var stringify = JSON.stringify.bind(JSON);
  var parse = JSON.parse.bind(JSON);
  var P = Promise;
  var pending = new Map();
  var seq = 0;
  var ERR = parse(@ERRORS@);
  var PARAMS = @PARAMS@;
  var CAPS = parse(@CAPS@);
  function request(m) {
    return new P(function (resolve, reject) {
      var id = ++seq;
      pending.set(id, { resolve: resolve, reject: reject });
      m.id = id;
      try { post(stringify(m)); } catch (e) { pending.delete(id); reject(new Error(ERR.refused)); }
    });
  }
  Object.defineProperty(window, '__vibeReply', {
    value: function (id, ok, v) {
      var p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (ok) p.resolve(v); else p.reject(new Error(ERR[v] || ERR.refused));
    }
  });
  var api = Object.freeze({
    run: function (query, values) {
      if (typeof query !== 'string') return P.reject(new Error(ERR.refused));
      var m = { t: 'run', q: query };
      if (values !== undefined && values !== null) m.v = values;
      return request(m);
    },
    params: function () { return parse(PARAMS); },
    // A capability is an object only when Studio granted it (CAPS) and a later
    // step built it; until then every name resolves to null, as one the page
    // did not declare does.
    use: function (name) { return P.resolve(null); }
  });
  Object.defineProperty(window, 'vibe', { value: api });
  void CAPS;
  var beat = window.setInterval.bind(window);
  beat(function () { try { post('{"t":"hb"}'); } catch (e) {} }, 500);
})();`
