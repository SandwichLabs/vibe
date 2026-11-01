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
//	slaw.run(query, values)   Promise of {columns, rows, truncated}
//	slaw.params()             the declared parameters, defaults and options
//	slaw.use(name)            Promise of a capability object, or null
//
// A capability object exists only for a name in caps (what the manifest
// declared and Studio granted): state, download and ask (H5). Each method maps
// to one call message; an argument too big for a call goes as put chunks first.
//
// It also posts a heartbeat every 500 ms, which is what Studio's watchdog
// listens for. The page is untrusted and may replace anything it can reach, so
// the script keeps its own references to what it calls and defines its two
// globals as non-writable, non-configurable. A page that breaks the script
// breaks only itself (and its heartbeat, which stops the window).
//
// paramsJSON is a JSON object (the manifest's parameters); caps are the
// capabilities Studio grants.
// Both reach the script as string literals handed to JSON.parse, never as
// script text.
func APIScript(paramsJSON []byte, caps []string) string {
	sorted := append([]string{}, caps...)
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

// EventScript is the script the helper evaluates to deliver a capability's
// event to the page (state.onChange): the argument is JSON handed over as a
// string literal, and the page sees untrusted data.
func EventScript(capName string, arg []byte) string {
	if len(arg) == 0 {
		arg = []byte("null")
	}
	return fmt.Sprintf("window.__slawEvent(%s,JSON.parse(%s))", jsLiteral([]byte(capName)), jsLiteral(arg))
}

// ReplyScript is the script the helper evaluates to answer a pending request
// from the page: the id, and either the result (JSON, handed over as a string
// literal so a key such as "__proto__" stays data) or an error code.
func ReplyScript(id int64, ok bool, result []byte, code string) string {
	if ok {
		return fmt.Sprintf("window.__slawReply(%d,true,JSON.parse(%s))", id, jsLiteral(result))
	}
	return fmt.Sprintf("window.__slawReply(%d,false,%s)", id, jsLiteral([]byte(code)))
}

const apiTemplate = `(function () {
  'use strict';
  var handler = window.webkit.messageHandlers.slaw;
  var post = handler.postMessage.bind(handler);
  var stringify = JSON.stringify.bind(JSON);
  var parse = JSON.parse.bind(JSON);
  var P = Promise;
  var E = Error;
  var pending = new Map();
  var seq = 0;
  var ERR = parse(@ERRORS@);
  var PARAMS = @PARAMS@;
  var CAPS = parse(@CAPS@);
  var te = new TextEncoder();
  var encode = te.encode.bind(te);
  var toB64 = window.btoa.bind(window);
  var fromCode = String.fromCharCode;
  var isView = ArrayBuffer.isView.bind(ArrayBuffer);
  var blobBuffer = Blob.prototype.arrayBuffer;
  var now = Date.now.bind(Date);
  // The gesture. navigator.userActivation.isActive cannot be the check: the
  // host's own script evaluation (the reply to a call) leaves the page active
  // for seconds, measured in WebKitGTK 2.5x, so any page could save by waiting
  // for a reply. A trusted input event cannot be made by page script (isTrusted
  // is set by the engine), and this listener runs before any the page adds.
  var lastInput = 0;
  var GESTURE_MS = 5000;
  function onInput(ev) {
    if (ev.isTrusted !== true) return;
    if (ev.type === 'keydown' && (ev.key === 'Escape' || ev.repeat)) return;
    lastInput = now();
  }
  var inputs = ['keydown', 'mousedown', 'pointerdown', 'pointerup', 'touchend', 'click'];
  for (var k = 0; k < inputs.length; k++) window.addEventListener(inputs[k], onInput, true);
  function takeGesture() {
    var t = lastInput;
    lastInput = 0;
    return t !== 0 && now() - t <= GESTURE_MS;
  }
  var listeners = [];
  function fail(code) { return P.reject(new E(ERR[code] || ERR.refused)); }
  function request(m) {
    return new P(function (resolve, reject) {
      var id = ++seq;
      pending.set(id, { resolve: resolve, reject: reject });
      m.id = id;
      try { post(stringify(m)); } catch (e) { pending.delete(id); reject(new E(ERR.refused)); }
    });
  }
  Object.defineProperty(window, '__slawReply', {
    value: function (id, ok, v) {
      var p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (ok) p.resolve(v); else p.reject(new E(ERR[v] || ERR.refused));
    }
  });
  Object.defineProperty(window, '__slawEvent', {
    value: function (name, arg) {
      if (name !== 'state') return;
      for (var i = 0; i < listeners.length; i++) {
        try { listeners[i](arg); } catch (e) {}
      }
    }
  });
  // An argument over a few KB goes as text chunks, each acknowledged before the
  // next (a chunk is at most 60 KB on the wire), then the call names the upload.
  var upSeq = 0;
  function putAll(text) {
    var up = ++upSeq, pos = 0, n = 0;
    if (text.length > 30000000) return fail('toolarge');
    return new P(function (resolve, reject) {
      function step() {
        var size = 56000, end, m, line;
        for (;;) {
          end = Math.min(text.length, pos + size);
          if (end < text.length) {
            var c = text.charCodeAt(end - 1);
            if (c >= 0xD800 && c <= 0xDBFF) end--;
          }
          m = { t: 'put', id: 2147483647, up: up, s: text.slice(pos, end) };
          if (n > 0) m.seq = n;
          if (end >= text.length) m.last = true;
          line = stringify(m);
          if (encode(line).length <= 62000 || size <= 64) break;
          size = size >> 1;
        }
        request(m).then(function () {
          pos = end;
          n++;
          if (pos >= text.length) resolve(up); else step();
        }, reject);
      }
      step();
    });
  }
  function call(cap, op, arg) {
    var text = '';
    try { text = arg === undefined ? '' : (stringify(arg) || ''); } catch (e) { return fail('refused'); }
    if (text.length <= 6000) {
      var m = { t: 'call', cap: cap, op: op };
      if (text) m.arg = parse(text);
      return request(m);
    }
    return putAll(text).then(function (up) { return request({ t: 'call', cap: cap, op: op, up: up }); });
  }
  function str(v) { return typeof v === 'string'; }
  function b64(bytes) {
    var s = '';
    for (var i = 0; i < bytes.length; i += 32768) s += fromCode.apply(null, bytes.subarray(i, i + 32768));
    return toB64(s);
  }
  function bytesOf(d) {
    if (d instanceof ArrayBuffer) return new Uint8Array(d);
    return new Uint8Array(d.buffer, d.byteOffset, d.byteLength);
  }
  var kinds = {
    state: {
      get: function (key) { return str(key) ? call('state', 'get', { key: key }) : fail('refused'); },
      set: function (key, value) {
        if (!str(key) || value === undefined) return fail('refused');
        return call('state', 'set', { key: key, value: value });
      },
      delete: function (key) { return str(key) ? call('state', 'delete', { key: key }) : fail('refused'); },
      list: function (prefix) {
        if (prefix !== undefined && !str(prefix)) return fail('refused');
        return call('state', 'list', { prefix: prefix === undefined ? '' : prefix });
      },
      onChange: function (fn) {
        if (typeof fn !== 'function') return function () {};
        listeners.push(fn);
        return function () { var i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); };
      }
    },
    download: {
      save: function (o) {
        // Read inside the web view, now, before anything the page defines can run;
        // one input pays for one save.
        if (!takeGesture()) return fail('gesture');
        if (!o || !str(o.filename) || !str(o.type)) return fail('refused');
        var d = o.data;
        var send = function (enc, data) { return call('download', 'save', { filename: o.filename, type: o.type, enc: enc, data: data }); };
        if (str(d)) return send('text', d);
        if (d instanceof ArrayBuffer || isView(d)) return send('base64', b64(bytesOf(d)));
        if (d instanceof Blob) {
          return blobBuffer.call(d).then(function (buf) { return send('base64', b64(new Uint8Array(buf))); });
        }
        return fail('refused');
      }
    },
    ask: {
      ask: function (prompt, o) {
        if (!str(prompt)) return fail('refused');
        var a = { prompt: prompt };
        if (o && o.system !== undefined) {
          if (!str(o.system)) return fail('refused');
          a.system = o.system;
        }
        return call('ask', 'ask', a);
      }
    }
  };
  var granted = {};
  for (var i = 0; i < CAPS.length; i++) {
    if (Object.prototype.hasOwnProperty.call(kinds, CAPS[i])) granted[CAPS[i]] = Object.freeze(kinds[CAPS[i]]);
  }
  var api = Object.freeze({
    run: function (query, values) {
      if (typeof query !== 'string') return fail('refused');
      var m = { t: 'run', q: query };
      if (values !== undefined && values !== null) m.v = values;
      return request(m);
    },
    params: function () { return parse(PARAMS); },
    // A capability is an object only when the manifest declared it and Studio
    // granted it; every other name resolves to null, and a page renders without.
    use: function (name) {
      if (typeof name !== 'string' || !Object.prototype.hasOwnProperty.call(granted, name)) return P.resolve(null);
      return P.resolve(granted[name]);
    }
  });
  Object.defineProperty(window, 'slaw', { value: api });
  var beat = window.setInterval.bind(window);
  beat(function () { try { post('{"t":"hb"}'); } catch (e) {} }, 500);
})();`
