# Minimal MCP stdio client: initialize, then write_artifact with view: html and html_files, then get_artifact.
import json, subprocess, sys
vibe, ws = sys.argv[1], sys.argv[2]
p = subprocess.Popen([vibe, 'mcp', 'serve', '--workspace', ws], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
def rpc(i, method, params):
    p.stdin.write(json.dumps({'jsonrpc': '2.0', 'id': i, 'method': method, 'params': params}) + '\n'); p.stdin.flush()
    return json.loads(p.stdout.readline())
print('init:', json.dumps(rpc(1, 'initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'mac-qa', 'version': '1'}}))[:200])
p.stdin.write(json.dumps({'jsonrpc': '2.0', 'method': 'notifications/initialized'}) + '\n'); p.stdin.flush()
manifest = {'version': 1, 'title': 'Agent page', 'view': 'html',
            'parameters': [{'name': 'msg', 'type': 'text', 'max_length': 200, 'default': ''}],
            'queries': [{'name': 'report', 'sql': 'SELECT $msg AS msg'}, {'name': 'n', 'sql': 'SELECT 42 AS answer'}]}
files = {'index.html.tmpl': '<!doctype html><html lang="en"><head><meta charset="utf-8"><title>agent page</title><link rel="stylesheet" href="assets/s.css"></head><body><h1>agent page</h1><p>answer {{ $q := query "n" }}{{ range $q.Rows }}{{ index . 0 }}{{ end }}</p><script src="a.js"></script></body></html>',
         'a.js': "vibe.run('report', {msg: 'agent page script ran'}).catch(() => {})\n",
         'assets/s.css': 'body { font: 14px system-ui; }\n'}
r = rpc(2, 'tools/call', {'name': 'write_artifact', 'arguments': {'name': 'agentpage', 'manifest': manifest, 'html_files': files}})
print('write_artifact:', json.dumps(r)[:600])
r = rpc(3, 'tools/call', {'name': 'get_artifact', 'arguments': {'name': 'agentpage'}})
print('get_artifact:', json.dumps(r)[:600])
p.stdin.close(); p.wait(timeout=10); print('stderr:', p.stderr.read()[:400])
