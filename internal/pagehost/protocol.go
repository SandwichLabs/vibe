package pagehost

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"sync"

	"github.com/SandwichLabs/slaw/internal/jsonenc"
)

// HelperArg is the first argument that puts the Studio binary in helper mode.
const HelperArg = "--view-host"

// Descriptor numbers of the dedicated pipes in the helper (exec.Cmd.ExtraFiles
// starts at 3): it reads Studio's messages from FDIn and writes its own to FDOut.
const (
	FDIn  = 3
	FDOut = 4
)

// Sizes. A page can post at most MaxFromHelper bytes in one message, and Studio
// sends at most MaxToHelper bytes in one line; a file or a document larger than
// a line goes in chunks of ChunkBytes.
const (
	MaxFromHelper = 64 << 10
	MaxToHelper   = 2 << 20
	ChunkBytes    = 1 << 20

	// What the helper will hold from Studio, however many messages arrive.
	MaxFiles      = 400
	MaxFileBytes  = 16 << 20
	MaxTotalBytes = 96 << 20
	MaxDocBytes   = 10 << 20
)

// Message types. A type belongs to one direction only.
const (
	// Helper to Studio.
	TypeHeartbeat = "hb"     // the page is alive (posted by the injected script)
	TypeReady     = "ready"  // the first document finished loading
	TypeRun       = "run"    // slaw.run(query, values)
	TypeDoc       = "doc"    // a navigation asks for the entry document with a new query string
	TypeCall      = "call"   // slaw.use(name).op(arg): a capability call
	TypePut       = "put"    // one chunk of a capability argument too big for a call (acked with a reply)
	TypeGone      = "gone"   // the web process died
	TypeClosed    = "closed" // the person closed the window

	// Studio to helper.
	TypeOpen  = "open"  // title, size, parameters, query string
	TypeFile  = "file"  // one chunk of one served file
	TypeGo    = "go"    // everything is sent: open the window
	TypeReply = "reply" // the answer to a run, a put or a call
	TypePush  = "push"  // an event for the page from a capability (state: another window wrote)
	TypeDocr  = "docr"  // one chunk of an entry document
	TypeQuit  = "quit"  // close the window and exit
)

// Error codes of a reply: a closed set, each with a fixed text. A reply never
// carries a path, a policy reason or an error from below.
const (
	CodeLimit       = "limit"
	CodeQuery       = "query"
	CodeRefused     = "refused"
	CodeTooLarge    = "toolarge"
	CodeUnavailable = "unavailable"
	CodeStopped     = "stopped"
	CodeGesture     = "gesture"
	CodeModel       = "model"
)

// ErrorText is the fixed text a page sees for an error code.
var ErrorText = map[string]string{
	CodeLimit:       "This page is asking for data too quickly.",
	CodeQuery:       "That query could not run.",
	CodeRefused:     "That request was not allowed.",
	CodeTooLarge:    "That result is too large.",
	CodeUnavailable: "That is not available.",
	CodeStopped:     "This page was stopped.",
	CodeGesture:     "Saving a file needs a click or a key press first.",
	CodeModel:       "The model could not answer.",
}

// Message is every message of the protocol; which fields a type may carry, and
// which it must carry, is the shape table below. Fields are short on purpose:
// a page-originated message is capped at 64 KB.
type Message struct {
	T string `json:"t"`
	// ID pairs a request (run, doc, call) with its reply. Positive.
	ID int64 `json:"id,omitempty"`
	// Q is a query name (run) or a raw query string (doc and open).
	Q string `json:"q,omitempty"`
	// V holds the values of a run: scalars only.
	V map[string]any `json:"v,omitempty"`
	// Cap, Op and Arg are a capability call; Up names an upload (put chunks)
	// whose assembled text is the argument; Text is a put's chunk.
	Cap  string          `json:"cap,omitempty"`
	Op   string          `json:"op,omitempty"`
	Arg  json.RawMessage `json:"arg,omitempty"`
	Up   int64           `json:"up,omitempty"`
	Text string          `json:"s,omitempty"`

	// Open.
	Name   string          `json:"name,omitempty"`
	Title  string          `json:"title,omitempty"`
	W      int             `json:"w,omitempty"`
	H      int             `json:"h,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Caps   []string        `json:"caps,omitempty"`

	// File and Docr: one chunk. Seq counts from zero and Last ends the file.
	Path string `json:"path,omitempty"`
	CT   string `json:"ct,omitempty"`
	Seq  int    `json:"seq,omitempty"`
	Last bool   `json:"last,omitempty"`
	Data []byte `json:"data,omitempty"`
	// Status is the HTTP-ish status of a document (200 or an error page's).
	Status int `json:"status,omitempty"`

	// Reply.
	OK     *bool           `json:"ok,omitempty"`
	Code   string          `json:"code,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Direction is which end sent a message.
type Direction int

// The two directions.
const (
	FromHelper Direction = iota
	ToHelper
)

func (d Direction) max() int {
	if d == FromHelper {
		return MaxFromHelper
	}
	return MaxToHelper
}

type shape struct {
	dir      Direction
	required []string
	optional []string
}

var shapes = map[string]shape{
	TypeHeartbeat: {dir: FromHelper},
	TypeReady:     {dir: FromHelper},
	TypeRun:       {dir: FromHelper, required: []string{"id", "q"}, optional: []string{"v"}},
	TypeDoc:       {dir: FromHelper, required: []string{"id"}, optional: []string{"q"}},
	TypeCall:      {dir: FromHelper, required: []string{"id", "cap", "op"}, optional: []string{"arg", "up"}},
	TypePut:       {dir: FromHelper, required: []string{"id", "up", "s"}, optional: []string{"seq", "last"}},
	TypeGone:      {dir: FromHelper},
	TypeClosed:    {dir: FromHelper},

	TypeOpen:  {dir: ToHelper, required: []string{"name", "title", "w", "h", "params"}, optional: []string{"q", "caps"}},
	TypeFile:  {dir: ToHelper, required: []string{"path", "ct"}, optional: []string{"seq", "last", "data"}},
	TypeGo:    {dir: ToHelper},
	TypeReply: {dir: ToHelper, required: []string{"id", "ok"}, optional: []string{"code", "result"}},
	TypePush:  {dir: ToHelper, required: []string{"cap", "op"}, optional: []string{"arg"}},
	TypeDocr:  {dir: ToHelper, required: []string{"status"}, optional: []string{"id", "seq", "last", "data"}},
	TypeQuit:  {dir: ToHelper},
}

var (
	nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	// pathRe is a served file's path: plain segments, at most three deep, with
	// the host's two reserved folders allowed (Studio, not the page, picks them).
	pathRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,99}(/[A-Za-z0-9_][A-Za-z0-9._-]{0,99}){0,2}$`)
	// hostNameRe is what an artifact name may be as a URL host: lower-case
	// letters, digits, hyphens and underscores. The artifact store's rule is
	// narrower; this is the protocol's bound.
	hostNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// ContentTypes is the closed list of types a served file may have.
var ContentTypes = map[string]bool{
	"text/javascript; charset=utf-8": true, "text/css; charset=utf-8": true, "image/svg+xml": true,
	"image/png": true, "image/jpeg": true, "image/webp": true, "font/woff2": true,
	"application/json; charset=utf-8": true,
}

// Decode reads one line sent by the end opposite dir's receiver: the line is
// at most the direction's cap, one JSON object, with a known type of that
// direction, exactly the fields its shape allows and none it does not, and
// every value in range. Anything else is an error, and the caller closes the
// pipe.
func Decode(line []byte, dir Direction) (*Message, error) {
	if len(line) > dir.max() {
		return nil, errors.New("pagehost: message too large")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(line, &keys); err != nil {
		return nil, errors.New("pagehost: not a JSON object")
	}
	var t string
	if raw, ok := keys["t"]; !ok || json.Unmarshal(raw, &t) != nil {
		return nil, errors.New("pagehost: no message type")
	}
	sh, ok := shapes[t]
	if !ok || sh.dir != dir {
		return nil, fmt.Errorf("pagehost: unknown message type %q", truncate(t))
	}
	if err := checkKeys(keys, sh); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var m Message
	if err := dec.Decode(&m); err != nil {
		return nil, errors.New("pagehost: a field has the wrong type")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func checkKeys(keys map[string]json.RawMessage, sh shape) error {
	allowed := map[string]bool{"t": true}
	for _, k := range sh.required {
		allowed[k] = true
		if _, ok := keys[k]; !ok {
			return fmt.Errorf("pagehost: missing field %q", k)
		}
	}
	for _, k := range sh.optional {
		allowed[k] = true
	}
	var extra []string
	for k := range keys {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("pagehost: field %q is not part of this message", truncate(extra[0]))
	}
	return nil
}

// Encode returns the line (with its newline) for m, refusing a message that
// Decode would refuse, or that is over its direction's cap.
func Encode(m *Message, dir Direction) ([]byte, error) {
	if sh, ok := shapes[m.T]; !ok || sh.dir != dir {
		return nil, fmt.Errorf("pagehost: unknown message type %q", truncate(m.T))
	}
	// HTML escaping off: a "<" in a value stays one byte, so a message is the
	// size its fields were measured at (the pipe is not HTML).
	b, err := jsonenc.Encode(m, "")
	if err != nil {
		return nil, fmt.Errorf("pagehost: failed to encode: %w", err)
	}
	b = bytes.TrimRight(b, "\n")
	if len(b) >= dir.max() {
		return nil, errors.New("pagehost: message too large")
	}
	// Decode is the one definition of a valid message.
	if _, err := Decode(b, dir); err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func truncate(s string) string {
	if len(s) > 32 {
		return s[:32] + "..."
	}
	return s
}

// LineReader reads newline-terminated lines of at most max bytes.
type LineReader struct {
	br  *bufio.Reader
	max int
}

// NewLineReader reads lines from r, none longer than dir's cap.
func NewLineReader(r io.Reader, dir Direction) *LineReader {
	return &LineReader{br: bufio.NewReaderSize(r, 64<<10), max: dir.max() + 1}
}

// ErrLineTooLong is a line over the cap: the stream is not trusted afterwards.
var ErrLineTooLong = errors.New("pagehost: line too long")

// Next returns the next line without its newline. io.EOF is the other end
// closing the pipe.
func (l *LineReader) Next() ([]byte, error) {
	var out []byte
	for {
		part, err := l.br.ReadSlice('\n')
		out = append(out, part...)
		if len(out) > l.max {
			return nil, ErrLineTooLong
		}
		switch {
		case err == nil:
			return bytes.TrimRight(out, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, err
		}
	}
}

// Writer writes encoded lines to one end of the pipe, one at a time.
type Writer struct {
	mu  sync.Mutex
	w   io.Writer
	dir Direction
}

// NewWriter writes messages of the direction dir to w.
func NewWriter(w io.Writer, dir Direction) *Writer { return &Writer{w: w, dir: dir} }

// Send encodes and writes m.
func (w *Writer) Send(m *Message) error {
	line, err := Encode(m, w.dir)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(line); err != nil {
		return fmt.Errorf("pagehost: failed to write: %w", err)
	}
	return nil
}

// CSP is the policy every response of the page's scheme carries (H4). No
// 'unsafe-eval' and no inline script: data reaches script through a JSON block.
// 'self' is slaw-page://<name>/, which serves only bytes Studio sent, and
// nothing it serves runs a query: queries go through slaw.run.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; worker-src blob:; frame-src 'none'; form-action 'none'; base-uri 'none'"
