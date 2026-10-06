package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/filestore"
	"github.com/sandwich-labs/vibe-app/internal/workspace"
)

type exportItem struct {
	Entry
	data []byte
}

// Export builds a bundle from the selected content of t. Nothing here reads
// credentials, sign-in choices or data: it reads the stores' own files and the
// connection definitions, and sanitises the latter.
func Export(ctx context.Context, t Target, opts ExportOptions) (*ExportResult, error) {
	sel := opts.Selection
	var (
		items    []exportItem
		warnings []string
	)
	add := func(kind ItemKind, id string, explicit bool) error {
		rel := entryPath(kind, id)
		data, _, err := filestore.Read(t.Root(), rel)
		if err == nil {
			_, _, err = validateItem(kind, id, data)
		}
		if err != nil {
			if explicit {
				return fmt.Errorf("cannot export %s: %w", Key(kind, id), err)
			}
			warnings = append(warnings, fmt.Sprintf("%s was skipped: %v", rel, err))
			return nil
		}
		sum := sha256.Sum256(data)
		items = append(items, exportItem{Entry{Path: rel, Kind: kind, ID: id, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}, data})
		return nil
	}

	type group struct {
		kind ItemKind
		ids  []string
		list func() ([]string, error)
	}
	groups := []group{
		{KindNotebook, sel.Notebooks, func() ([]string, error) {
			l, err := t.Notebooks().List(ctx)
			ids := make([]string, 0, len(l))
			for _, s := range l {
				ids = append(ids, s.ID)
			}
			return ids, err
		}},
		{KindQuery, sel.Queries, func() ([]string, error) {
			l, err := t.Queries().List(ctx)
			ids := make([]string, 0, len(l))
			for _, q := range l {
				ids = append(ids, q.ID)
			}
			return ids, err
		}},
		{KindSkill, sel.Skills, func() ([]string, error) {
			l, err := t.Skills().List(ctx)
			ids := make([]string, 0, len(l))
			for _, s := range l {
				ids = append(ids, s.Name)
			}
			return ids, err
		}},
	}
	for _, g := range groups {
		ids, explicit := g.ids, true
		if sel.All {
			var err error
			if ids, err = g.list(); err != nil {
				return nil, fmt.Errorf("failed to list %ss: %w", g.kind, err)
			}
			explicit = false
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, _, err := classify(entryPath(g.kind, id)); err != nil {
				if explicit {
					return nil, fmt.Errorf("cannot export %s: not a valid id", Key(g.kind, id))
				}
				warnings = append(warnings, fmt.Sprintf("%s %q was skipped: not a valid id", g.kind, id))
				continue
			}
			if err := add(g.kind, id, explicit); err != nil {
				return nil, err
			}
		}
	}

	conns, connWarnings, err := collectConnections(t, sel)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, connWarnings...)
	if len(conns) > 0 {
		data, err := encodeConnections(conns)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		items = append(items, exportItem{Entry{Path: ConnectionsName, Kind: KindConnections, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}, data})
	}
	if len(items) == 0 {
		return nil, errors.New("nothing to export: select at least one notebook, query, skill or connection")
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	var total int64
	for _, it := range items {
		total += it.Size
	}
	if total > MaxTotalBytes {
		return nil, fmt.Errorf("%w: the selection is %d bytes, over the %d byte limit", ErrTooLarge, total, MaxTotalBytes)
	}

	created := time.Now().UTC().Format(time.RFC3339)
	if opts.Now != nil {
		created = opts.Now()
	}
	man := Manifest{
		Kind: Kind, FormatVersion: FormatVersion, CreatedAt: created,
		CreatedBy: opts.AppVersion, WorkspaceName: t.Name(), Warnings: warnings,
		Contents: make([]Entry, 0, len(items)),
	}
	for _, it := range items {
		man.Contents = append(man.Contents, it.Entry)
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode the manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Time{}})
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
		_, err = w.Write(data)
		return err
	}
	if err := put(ManifestName, append(mb, '\n')); err != nil {
		return nil, err
	}
	for _, it := range items {
		if err := put(it.Path, it.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish the bundle: %w", err)
	}

	res := &ExportResult{Data: buf.Bytes(), Manifest: man}
	if opts.Passphrase != "" {
		if res.Data, err = encrypt(res.Data, opts.Passphrase); err != nil {
			return nil, err
		}
		res.Encrypted = true
	}
	return res, nil
}

// collectConnections gathers the definitions the selection asks for, each
// sanitised. A definition that cannot be made portable is left out, with a
// warning in the manifest.
func collectConnections(t Target, sel Selection) ([]workspace.ConnectionDef, []string, error) {
	var (
		out      []workspace.ConnectionDef
		warnings []string
		have     = map[string]bool{}
	)
	take := func(d workspace.ConnectionDef, where string) {
		clean, notes, err := SanitizeConnection(d)
		warnings = append(warnings, notes...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("connection %q (%s) was left out: %v", d.Name, where, err))
			return
		}
		if have[clean.Name] {
			warnings = append(warnings, fmt.Sprintf("connection %q (%s) was left out: the name is already taken by the team definition", d.Name, where))
			return
		}
		have[clean.Name] = true
		out = append(out, clean)
	}
	if sel.SharedConnections || len(sel.TeamConnections) > 0 {
		want := map[string]bool{}
		for _, n := range sel.TeamConnections {
			want[n] = true
		}
		found := map[string]bool{}
		for _, d := range t.Config().Connections {
			if d.Name == workspace.DefaultConnectionName && d.Path == workspace.DefaultConnectionPath {
				continue // every workspace has its own; there is no data to carry to it
			}
			found[d.Name] = true
			if sel.SharedConnections || want[d.Name] {
				take(d, "team")
			}
		}
		for n := range want {
			if !found[n] {
				return nil, nil, fmt.Errorf("there is no team connection named %q", n)
			}
		}
	}
	if len(sel.LocalConnections) > 0 {
		local := map[string]workspace.ConnectionDef{}
		for _, d := range t.Local().Connections {
			local[d.Name] = d
		}
		for _, name := range sel.LocalConnections {
			d, ok := local[name]
			if !ok {
				return nil, nil, fmt.Errorf("there is no \"This computer\" connection named %q", name)
			}
			take(d, "this computer")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings, nil
}
