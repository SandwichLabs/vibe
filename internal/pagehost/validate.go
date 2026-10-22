package pagehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Value limits of a message.
const (
	maxValues      = 16
	maxValueString = 4096
	maxQueryString = 8 << 10
	MaxTitle       = 200
	maxCaps        = 8
	MinWindow      = 200
	MaxWindow      = 4000
	maxParamsBytes = 256 << 10
	maxArgBytes    = 32 << 10
	// MaxPutText is the most text a put chunk may carry, and MaxUploadBytes the
	// most text one assembled upload may hold (a 20 MB file as base64, plus its
	// envelope).
	MaxPutText     = 60 << 10
	MaxUploadBytes = 28 << 20
	maxPushArg     = 80 << 10
)

func (m *Message) validate() error {
	switch m.T {
	case TypeRun:
		return m.validateRun()
	case TypeDoc:
		return m.validateDoc()
	case TypeCall:
		return m.validateCall()
	case TypePut:
		return m.validatePut()
	case TypePush:
		return m.validatePush()
	case TypeOpen:
		return m.validateOpen()
	case TypeFile:
		return m.validateFile()
	case TypeReply:
		return m.validateReply()
	case TypeDocr:
		return m.validateDocr()
	}
	return nil
}

func (m *Message) validateID() error {
	if m.ID < 1 || m.ID > 1<<31 {
		return errors.New("pagehost: bad request id")
	}
	return nil
}

func (m *Message) validateRun() error {
	if err := m.validateID(); err != nil {
		return err
	}
	if !nameRe.MatchString(m.Q) {
		return errors.New("pagehost: bad query name")
	}
	if len(m.V) > maxValues {
		return errors.New("pagehost: too many values")
	}
	for k, v := range m.V {
		if !nameRe.MatchString(k) {
			return errors.New("pagehost: bad value name")
		}
		switch x := v.(type) {
		case nil, bool, float64:
		case string:
			if len(x) > maxValueString || !utf8.ValidString(x) {
				return errors.New("pagehost: bad value")
			}
		default:
			return errors.New("pagehost: a value must be a string, number, boolean or null")
		}
	}
	return nil
}

func validQueryString(s string) bool {
	if len(s) > maxQueryString || !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '#' })
}

func (m *Message) validateDoc() error {
	if err := m.validateID(); err != nil {
		return err
	}
	if !validQueryString(m.Q) {
		return errors.New("pagehost: bad query string")
	}
	return nil
}

func (m *Message) validateCall() error {
	if err := m.validateID(); err != nil {
		return err
	}
	if !nameRe.MatchString(m.Cap) || !nameRe.MatchString(m.Op) {
		return errors.New("pagehost: bad capability call")
	}
	if len(m.Arg) > maxArgBytes || (len(m.Arg) > 0 && !json.Valid(m.Arg)) {
		return errors.New("pagehost: bad capability argument")
	}
	if m.Up != 0 && (len(m.Arg) > 0 || m.Up < 1 || m.Up > 1<<31) {
		return errors.New("pagehost: bad capability upload")
	}
	return nil
}

func (m *Message) validatePut() error {
	if err := m.validateID(); err != nil {
		return err
	}
	if m.Up < 1 || m.Up > 1<<31 {
		return errors.New("pagehost: bad upload id")
	}
	if m.Seq < 0 || m.Seq > 1<<16 {
		return errors.New("pagehost: bad chunk number")
	}
	if len(m.Text) > MaxPutText || !utf8.ValidString(m.Text) {
		return errors.New("pagehost: bad upload chunk")
	}
	return nil
}

func (m *Message) validatePush() error {
	if !nameRe.MatchString(m.Cap) || !nameRe.MatchString(m.Op) {
		return errors.New("pagehost: bad event")
	}
	if len(m.Arg) > maxPushArg || (len(m.Arg) > 0 && !json.Valid(m.Arg)) {
		return errors.New("pagehost: bad event argument")
	}
	return nil
}

func (m *Message) validateOpen() error {
	if !hostNameRe.MatchString(m.Name) {
		return errors.New("pagehost: bad page name")
	}
	if m.Title == "" || len(m.Title) > MaxTitle || !utf8.ValidString(m.Title) || strings.ContainsFunc(m.Title, isControl) {
		return errors.New("pagehost: bad title")
	}
	if m.W < MinWindow || m.W > MaxWindow || m.H < MinWindow || m.H > MaxWindow {
		return errors.New("pagehost: bad window size")
	}
	if len(m.Params) > maxParamsBytes || !json.Valid(m.Params) || m.Params[0] != '{' {
		return errors.New("pagehost: bad parameters")
	}
	if !validQueryString(m.Q) {
		return errors.New("pagehost: bad query string")
	}
	if len(m.Caps) > maxCaps {
		return errors.New("pagehost: too many capabilities")
	}
	for _, c := range m.Caps {
		if !nameRe.MatchString(c) {
			return errors.New("pagehost: bad capability name")
		}
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

func (m *Message) validateChunk() error {
	if m.Seq < 0 || m.Seq > 1<<16 {
		return errors.New("pagehost: bad chunk number")
	}
	if len(m.Data) > ChunkBytes {
		return errors.New("pagehost: chunk too large")
	}
	return nil
}

func (m *Message) validateFile() error {
	if !pathRe.MatchString(m.Path) || strings.Contains(m.Path, "..") {
		return fmt.Errorf("pagehost: bad file path %q", truncate(m.Path))
	}
	if !ContentTypes[m.CT] {
		return errors.New("pagehost: bad content type")
	}
	return m.validateChunk()
}

func (m *Message) validateReply() error {
	if err := m.validateID(); err != nil {
		return err
	}
	if m.OK == nil {
		return errors.New("pagehost: reply without ok")
	}
	if *m.OK {
		if m.Code != "" || len(m.Result) == 0 || !json.Valid(m.Result) {
			return errors.New("pagehost: bad reply")
		}
		return nil
	}
	if _, ok := ErrorText[m.Code]; !ok || len(m.Result) != 0 {
		return errors.New("pagehost: bad reply")
	}
	return nil
}

func (m *Message) validateDocr() error {
	if m.ID < 0 || m.ID > 1<<31 {
		return errors.New("pagehost: bad request id")
	}
	if m.Status != 200 && (m.Status < 400 || m.Status > 599) {
		return errors.New("pagehost: bad status")
	}
	return m.validateChunk()
}
