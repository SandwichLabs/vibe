package pagehost

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeAcceptsEachShape(t *testing.T) {
	cases := []struct {
		name string
		line string
		dir  Direction
	}{
		{"hb", `{"t":"hb"}`, FromHelper},
		{"ready", `{"t":"ready"}`, FromHelper},
		{"run", `{"t":"run","id":3,"q":"by_region","v":{"from":"2026-01-01","n":2,"on":true,"x":null}}`, FromHelper},
		{"run no values", `{"t":"run","id":3,"q":"by_region"}`, FromHelper},
		{"doc", `{"t":"doc","id":4,"q":"region=east"}`, FromHelper},
		{"call", `{"t":"call","id":5,"cap":"state","op":"get","arg":{"key":"a"}}`, FromHelper},
		{"gone", `{"t":"gone"}`, FromHelper},
		{"closed", `{"t":"closed"}`, FromHelper},
		{"open", `{"t":"open","name":"sales_page","title":"Sales","w":900,"h":700,"params":{}}`, ToHelper},
		{"file", `{"t":"file","path":"assets/app.css","ct":"text/css; charset=utf-8","data":"YQ==","last":true}`, ToHelper},
		{"go", `{"t":"go"}`, ToHelper},
		{"reply ok", `{"t":"reply","id":3,"ok":true,"result":{"columns":[],"rows":[],"truncated":false}}`, ToHelper},
		{"reply err", `{"t":"reply","id":3,"ok":false,"code":"limit"}`, ToHelper},
		{"docr", `{"t":"docr","status":200,"data":"YQ==","last":true}`, ToHelper},
		{"quit", `{"t":"quit"}`, ToHelper},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode([]byte(c.line), c.dir)
			assert.NoError(t, err)
		})
	}
}

func TestDecodeRefusesBadShapes(t *testing.T) {
	cases := map[string]struct {
		line string
		dir  Direction
	}{
		"not json":                   {`hello`, FromHelper},
		"an array":                   {`[1]`, FromHelper},
		"no type":                    {`{"id":1}`, FromHelper},
		"unknown type":               {`{"t":"eval","id":1}`, FromHelper},
		"a helper type to helper":    {`{"t":"hb"}`, ToHelper},
		"a studio type from page":    {`{"t":"quit"}`, FromHelper},
		"an extra field":             {`{"t":"hb","artifact":"other"}`, FromHelper},
		"a field naming an artifact": {`{"t":"run","id":1,"q":"a","artifact":"other"}`, FromHelper},
		"a field of another type":    {`{"t":"run","id":1,"q":"a","path":"x"}`, FromHelper},
		"missing id":                 {`{"t":"run","q":"a"}`, FromHelper},
		"zero id":                    {`{"t":"run","id":0,"q":"a"}`, FromHelper},
		"negative id":                {`{"t":"run","id":-4,"q":"a"}`, FromHelper},
		"string id":                  {`{"t":"run","id":"1","q":"a"}`, FromHelper},
		"query with a space":         {`{"t":"run","id":1,"q":"a b"}`, FromHelper},
		"query with SQL":             {`{"t":"run","id":1,"q":"x; drop table t"}`, FromHelper},
		"empty query name":           {`{"t":"run","id":1,"q":""}`, FromHelper},
		"object value":               {`{"t":"run","id":1,"q":"a","v":{"x":{"y":1}}}`, FromHelper},
		"array value":                {`{"t":"run","id":1,"q":"a","v":{"x":[1]}}`, FromHelper},
		"bad value name":             {`{"t":"run","id":1,"q":"a","v":{"x y":1}}`, FromHelper},
		"control in doc query":       {`{"t":"doc","id":1,"q":"a=\u0001"}`, FromHelper},
		"fragment in doc query":      {`{"t":"doc","id":1,"q":"a=1#x"}`, FromHelper},
		"call arg not json":          {`{"t":"call","id":1,"cap":"state","op":"get","arg":}`, FromHelper},
		"call with bad cap name":     {`{"t":"call","id":1,"cap":"../state","op":"get"}`, FromHelper},
		"open bad name":              {`{"t":"open","name":"../x","title":"T","w":900,"h":700,"params":{}}`, ToHelper},
		"open name as a host":        {`{"t":"open","name":"a.b","title":"T","w":900,"h":700,"params":{}}`, ToHelper},
		"open tiny window":           {`{"t":"open","name":"a","title":"T","w":10,"h":700,"params":{}}`, ToHelper},
		"open params array":          {`{"t":"open","name":"a","title":"T","w":900,"h":700,"params":[]}`, ToHelper},
		"open title newline":         {`{"t":"open","name":"a","title":"T\nU","w":900,"h":700,"params":{}}`, ToHelper},
		"open empty title":           {`{"t":"open","name":"a","title":"","w":900,"h":700,"params":{}}`, ToHelper},
		"file path escapes":          {`{"t":"file","path":"../etc/passwd","ct":"text/css; charset=utf-8"}`, ToHelper},
		"file absolute path":         {`{"t":"file","path":"/etc/passwd","ct":"text/css; charset=utf-8"}`, ToHelper},
		"file dotfile":               {`{"t":"file","path":".env","ct":"text/css; charset=utf-8"}`, ToHelper},
		"file too deep":              {`{"t":"file","path":"a/b/c/d.css","ct":"text/css; charset=utf-8"}`, ToHelper},
		"file html content type":     {`{"t":"file","path":"a.html","ct":"text/html"}`, ToHelper},
		"file negative seq":          {`{"t":"file","path":"a.css","ct":"text/css; charset=utf-8","seq":-1}`, ToHelper},
		"reply without ok":           {`{"t":"reply","id":1}`, ToHelper},
		"reply ok without result":    {`{"t":"reply","id":1,"ok":true}`, ToHelper},
		"reply error free text":      {`{"t":"reply","id":1,"ok":false,"code":"/home/zac/.ssh/id_rsa"}`, ToHelper},
		"reply error with result":    {`{"t":"reply","id":1,"ok":false,"code":"query","result":{}}`, ToHelper},
		"docr bad status":            {`{"t":"docr","status":302}`, ToHelper},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(c.line), c.dir)
			assert.Error(t, err)
		})
	}
}

func TestDecodeCaps(t *testing.T) {
	big := `{"t":"run","id":1,"q":"a","v":{"x":"` + strings.Repeat("a", MaxFromHelper) + `"}}`
	_, err := Decode([]byte(big), FromHelper)
	assert.ErrorContains(t, err, "too large", "a page message is at most 64 KB")

	long := `{"t":"run","id":1,"q":"a","v":{"x":"` + strings.Repeat("a", maxValueString+1) + `"}}`
	_, err = Decode([]byte(long), FromHelper)
	assert.Error(t, err, "a string value has its own cap")

	many := map[string]any{}
	for i := 0; i < maxValues+1; i++ {
		many["p"+strings.Repeat("a", i)] = 1
	}
	b, _ := json.Marshal(map[string]any{"t": "run", "id": 1, "q": "a", "v": many})
	_, err = Decode(b, FromHelper)
	assert.Error(t, err, "too many values")

	// Studio's lines may be larger, but not over 2 MB.
	huge := `{"t":"docr","status":200,"data":"` + strings.Repeat("A", MaxToHelper) + `"}`
	_, err = Decode([]byte(huge), ToHelper)
	assert.ErrorContains(t, err, "too large")
}

func TestEncodeRefusesWhatDecodeRefuses(t *testing.T) {
	_, err := Encode(&Message{T: TypeRun, ID: 1, Q: "x y"}, FromHelper)
	assert.Error(t, err)
	_, err = Encode(&Message{T: TypeQuit}, FromHelper)
	assert.Error(t, err, "a message of the other direction")
	_, err = Encode(&Message{T: TypeFile, Path: "a.js", CT: "text/javascript; charset=utf-8", Data: bytes.Repeat([]byte("x"), ChunkBytes+1)}, ToHelper)
	assert.Error(t, err, "a chunk over ChunkBytes")

	ok := true
	line, err := Encode(&Message{T: TypeReply, ID: 9, OK: &ok, Result: json.RawMessage(`{"a":1}`)}, ToHelper)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(line), "\n"))
	back, err := Decode(bytes.TrimSpace(line), ToHelper)
	require.NoError(t, err)
	require.NotNil(t, back.OK)
	assert.True(t, *back.OK)

	// A full-size chunk fits a line.
	_, err = Encode(&Message{T: TypeFile, Path: "a.js", CT: "text/javascript; charset=utf-8", Data: bytes.Repeat([]byte("x"), ChunkBytes), Last: true}, ToHelper)
	assert.NoError(t, err)
}

func TestMessageHasNoFieldNamingAnArtifact(t *testing.T) {
	// The protocol cannot name an artifact: Studio pins it when it opens the
	// window. The only name is the helper's own host (open.name), sent by Studio.
	for _, key := range []string{"artifact", "workspace", "root", "connection", "sql"} {
		line := `{"t":"run","id":1,"q":"a","` + key + `":"x"}`
		_, err := Decode([]byte(line), FromHelper)
		assert.Error(t, err, key)
	}
}

func TestLineReader(t *testing.T) {
	r := NewLineReader(strings.NewReader("{\"t\":\"hb\"}\n{\"t\":\"ready\"}\n"), FromHelper)
	a, err := r.Next()
	require.NoError(t, err)
	assert.Equal(t, `{"t":"hb"}`, string(a))
	b, err := r.Next()
	require.NoError(t, err)
	assert.Equal(t, `{"t":"ready"}`, string(b))
	_, err = r.Next()
	assert.ErrorIs(t, err, io.EOF)

	long := NewLineReader(strings.NewReader(strings.Repeat("a", MaxFromHelper+10)+"\n"), FromHelper)
	_, err = long.Next()
	assert.ErrorIs(t, err, ErrLineTooLong)

	atCap := NewLineReader(strings.NewReader(strings.Repeat("a", MaxFromHelper)+"\n"), FromHelper)
	line, err := atCap.Next()
	require.NoError(t, err)
	assert.Len(t, line, MaxFromHelper)
}

func TestWriterSendsOneLinePerMessage(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, FromHelper)
	require.NoError(t, w.Send(&Message{T: TypeHeartbeat}))
	require.NoError(t, w.Send(&Message{T: TypeReady}))
	assert.Equal(t, "{\"t\":\"hb\"}\n{\"t\":\"ready\"}\n", buf.String())
	assert.Error(t, w.Send(&Message{T: TypeQuit}))
}

func TestLimiter(t *testing.T) {
	var l Limiter
	now := time.Unix(1000, 0)
	for i := 0; i < MaxInFlight; i++ {
		require.True(t, l.Acquire(now), "in flight %d", i)
	}
	assert.False(t, l.Acquire(now), "a fifth request in flight")
	l.Release()
	assert.True(t, l.Acquire(now))
	for i := 0; i < MaxInFlight; i++ {
		l.Release()
	}

	// 60 a minute: sequential requests, each released at once.
	var m Limiter
	for i := 0; i < MaxPerMinute; i++ {
		require.True(t, m.Acquire(now.Add(time.Duration(i)*time.Second/2)), "request %d", i)
		m.Release()
	}
	assert.False(t, m.Acquire(now.Add(29*time.Second)), "the 61st in a minute")
	assert.True(t, m.Acquire(now.Add(61*time.Second)), "the window moves on")
}

func TestFlood(t *testing.T) {
	var f Flood
	now := time.Unix(2000, 0)
	for i := 0; i < MaxLinesPerSecond; i++ {
		require.False(t, f.Hit(now))
	}
	assert.True(t, f.Hit(now))
	assert.False(t, f.Hit(now.Add(time.Second)), "a new second starts at zero")
}

func TestErrorTextsAreFixed(t *testing.T) {
	for _, code := range []string{CodeLimit, CodeQuery, CodeRefused, CodeTooLarge, CodeUnavailable, CodeStopped, CodeGesture, CodeModel} {
		text := ErrorText[code]
		assert.NotEmpty(t, text, code)
		assert.NotContains(t, text, "/", "a text names no path: %s", code)
	}
}

func TestAPIScript(t *testing.T) {
	params := []byte(`{"region":{"type":"string","label":"</script><b>"}}`)
	js := APIScript(params, nil)
	assert.NotContains(t, js, "</script", "params arrive as a string literal handed to JSON.parse")
	assert.NotContains(t, js, "eval(")
	assert.Contains(t, js, "window.webkit.messageHandlers.assay")
	assert.Contains(t, js, `Object.defineProperty(window, 'assay'`)
	assert.Contains(t, js, `var CAPS = parse("[]")`, "no capability is granted unless Studio names it")
	assert.NotContains(t, APIScript(params, []string{"</script>"}), "</script", "caps arrive as a string literal too")
	assert.NotContains(t, js, "@PARAMS@")
	assert.NotContains(t, js, "@ERRORS@")
}

func TestReplyScript(t *testing.T) {
	ok := ReplyScript(7, true, []byte(`{"__proto__":{"x":1}}`), "")
	assert.True(t, strings.HasPrefix(ok, "window.__assayReply(7,true,JSON.parse(\""), "the result is data handed to JSON.parse: %s", ok)
	bad := ReplyScript(7, false, nil, CodeLimit)
	assert.Equal(t, `window.__assayReply(7,false,"limit")`, bad)
}

func TestPutPushAndUploadShapes(t *testing.T) {
	good := []string{
		`{"t":"put","id":1,"up":2,"s":"abc"}`,
		`{"t":"put","id":1,"up":2,"s":"abc","seq":3,"last":true}`,
		`{"t":"call","id":1,"cap":"state","op":"set","up":2}`,
	}
	for _, g := range good {
		_, err := Decode([]byte(g), FromHelper)
		assert.NoError(t, err, g)
	}
	bad := []string{
		`{"t":"put","id":1,"s":"abc"}`, // no upload id
		`{"t":"put","id":1,"up":0,"s":"abc"}`,
		`{"t":"put","id":1,"up":2,"s":"abc","path":"x"}`,               // a field it does not own
		`{"t":"call","id":1,"cap":"state","op":"set","up":2,"arg":{}}`, // an argument and an upload
		`{"t":"call","id":1,"cap":"state","op":"set","up":-1}`,
		`{"t":"push","cap":"state","op":"change"}`, // a push is Studio's, not a page's
	}
	for _, b := range bad {
		_, err := Decode([]byte(b), FromHelper)
		assert.Error(t, err, b)
	}
	_, err := Decode([]byte(`{"t":"push","cap":"state","op":"change","arg":{"key":"a"}}`), ToHelper)
	assert.NoError(t, err)
	_, err = Decode([]byte(`{"t":"push","cap":"st ate","op":"change"}`), ToHelper)
	assert.Error(t, err)
	_, err = Decode([]byte(`{"t":"put","id":1,"up":2,"s":"abc"}`), ToHelper)
	assert.Error(t, err, "Studio never sends a put")
	big := `{"t":"put","id":1,"up":2,"s":"` + strings.Repeat("a", MaxPutText+1) + `"}`
	_, err = Decode([]byte(big), FromHelper)
	assert.Error(t, err)
}

func TestEventScriptHandsDataOverAsALiteral(t *testing.T) {
	js := EventScript("state", []byte(`{"key":"</script>","value":{"__proto__":1}}`))
	assert.True(t, strings.HasPrefix(js, `window.__assayEvent("state",JSON.parse("`), js)
	assert.NotContains(t, js, "</script")
}

func TestRateAllowsNPerWindow(t *testing.T) {
	r := NewRate(3, time.Minute)
	now := time.Unix(1000, 0)
	assert.True(t, r.Allow(now))
	assert.True(t, r.Allow(now.Add(time.Second)))
	assert.True(t, r.Allow(now.Add(2*time.Second)))
	assert.False(t, r.Allow(now.Add(3*time.Second)))
	assert.True(t, r.Allow(now.Add(61*time.Second)), "the first aged out")
}

func TestEncodeDoesNotGrowHTMLCharacters(t *testing.T) {
	val := `"` + strings.Repeat("<&>", 20000) + `"` // 60 KB
	line, err := Encode(&Message{T: TypePush, Cap: "state", Op: "change", Arg: json.RawMessage(`{"key":"k","value":` + val + `}`)}, ToHelper)
	require.NoError(t, err, "a push of a 60 KB value of < & > is still under its cap")
	assert.Less(t, len(line), 80<<10)
	ok := true
	line, err = Encode(&Message{T: TypeReply, ID: 1, OK: &ok, Result: json.RawMessage(val)}, ToHelper)
	require.NoError(t, err)
	assert.Contains(t, string(line), "<&>")
}

func TestEncodeKeepsALineSeparatorValueTheSizeItWasMeasuredAt(t *testing.T) {
	// The state store keeps U+2028 in the 6-byte escaped form, so a 60 KB value
	// is 60 KB in a push too.
	val := `"` + strings.Repeat(` `, 10000) + `"`
	line, err := Encode(&Message{T: TypePush, Cap: "state", Op: "change", Arg: json.RawMessage(`{"key":"k","value":` + val + `}`)}, ToHelper)
	require.NoError(t, err)
	assert.Less(t, len(line), 80<<10)
}
