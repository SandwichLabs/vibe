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

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

type exportItem struct {
	Entry
	data []byte
}

// Export builds a bundle from the selected content of t. Nothing here reads
// credentials, sign-in choices or data: it reads the stores' own files and the
// connection definitions, and sanitises the latter.
func Export(ctx context.Context, t Target, opts ExportOptions) (*ExportResult, error) {
	ex := &exporter{t: t}
	if err := ex.collectStoreItems(ctx, opts.Selection); err != nil {
		return nil, err
	}
	if err := ex.collectConnectionItem(opts.Selection); err != nil {
		return nil, err
	}
	if len(ex.items) == 0 {
		return nil, errors.New("nothing to export: select at least one notebook, query, skill or connection")
	}
	sort.Slice(ex.items, func(i, j int) bool { return ex.items[i].Path < ex.items[j].Path })
	if err := ex.checkSize(); err != nil {
		return nil, err
	}

	man, err := ex.manifest(opts)
	if err != nil {
		return nil, err
	}
	data, err := ex.zip(man)
	if err != nil {
		return nil, err
	}
	res := &ExportResult{Data: data, Manifest: man.Manifest}
	if opts.Passphrase != "" {
		if res.Data, err = encrypt(res.Data, opts.Passphrase); err != nil {
			return nil, err
		}
		res.Encrypted = true
	}
	return res, nil
}

// exporter gathers the files of one Export.
type exporter struct {
	t        Target
	items    []exportItem
	warnings []string
}

// add reads and validates one stored item. An explicitly selected item that
// cannot be exported fails the export; a listed one is skipped with a warning.
func (ex *exporter) add(kind ItemKind, id string, explicit bool) error {
	rel := entryPath(kind, id)
	data, _, err := filestore.Read(ex.t.Root(), rel)
	if err == nil {
		_, _, err = validateItem(kind, id, data)
	}
	if err != nil {
		if explicit {
			return fmt.Errorf("cannot export %s: %w", Key(kind, id), err)
		}
		ex.warnings = append(ex.warnings, fmt.Sprintf("%s was skipped: %v", rel, err))
		return nil
	}
	ex.items = append(ex.items, exportItem{Entry{Path: rel, Kind: kind, ID: id, SHA256: sha256Hex(data), Size: int64(len(data))}, data})
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// exportGroup is one kind of store item: the ids the selection names and how
// to list every id of that kind.
type exportGroup struct {
	kind ItemKind
	ids  []string
	list func() ([]string, error)
}

func exportGroups(ctx context.Context, t Target, sel Selection) []exportGroup {
	return []exportGroup{
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
}

// collectStoreItems adds the selected notebooks, queries and skills.
func (ex *exporter) collectStoreItems(ctx context.Context, sel Selection) error {
	for _, g := range exportGroups(ctx, ex.t, sel) {
		ids, explicit := g.ids, true
		if sel.All {
			var err error
			if ids, err = g.list(); err != nil {
				return fmt.Errorf("failed to list %ss: %w", g.kind, err)
			}
			explicit = false
		}
		if err := ex.addGroup(g.kind, ids, explicit); err != nil {
			return err
		}
	}
	return nil
}

// addGroup adds each distinct id of one kind.
func (ex *exporter) addGroup(kind ItemKind, ids []string, explicit bool) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, _, err := classify(entryPath(kind, id)); err != nil {
			if explicit {
				return fmt.Errorf("cannot export %s: not a valid id", Key(kind, id))
			}
			ex.warnings = append(ex.warnings, fmt.Sprintf("%s %q was skipped: not a valid id", kind, id))
			continue
		}
		if err := ex.add(kind, id, explicit); err != nil {
			return err
		}
	}
	return nil
}

// collectConnectionItem adds connections.json when the selection has any.
func (ex *exporter) collectConnectionItem(sel Selection) error {
	conns, connWarnings, err := collectConnections(ex.t, sel)
	if err != nil {
		return err
	}
	ex.warnings = append(ex.warnings, connWarnings...)
	if len(conns) == 0 {
		return nil
	}
	data, err := encodeConnections(conns)
	if err != nil {
		return err
	}
	ex.items = append(ex.items, exportItem{Entry{Path: ConnectionsName, Kind: KindConnections, SHA256: sha256Hex(data), Size: int64(len(data))}, data})
	return nil
}

func (ex *exporter) checkSize() error {
	var total int64
	for _, it := range ex.items {
		total += it.Size
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("%w: the selection is %d bytes, over the %d byte limit", ErrTooLarge, total, MaxTotalBytes)
	}
	return nil
}

// builtManifest is the manifest and its encoded form.
type builtManifest struct {
	Manifest
	encoded []byte
}

func (ex *exporter) manifest(opts ExportOptions) (builtManifest, error) {
	created := time.Now().UTC().Format(time.RFC3339)
	if opts.Now != nil {
		created = opts.Now()
	}
	man := Manifest{
		Kind: Kind, FormatVersion: FormatVersion, CreatedAt: created,
		CreatedBy: opts.AppVersion, WorkspaceName: ex.t.Name(), Warnings: ex.warnings,
		Contents: make([]Entry, 0, len(ex.items)),
	}
	for _, it := range ex.items {
		man.Contents = append(man.Contents, it.Entry)
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return builtManifest{}, fmt.Errorf("failed to encode the manifest: %w", err)
	}
	return builtManifest{Manifest: man, encoded: append(mb, '\n')}, nil
}

// zip writes the manifest, then every item, into a deterministic archive.
func (ex *exporter) zip(man builtManifest) ([]byte, error) {
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
	if err := put(ManifestName, man.encoded); err != nil {
		return nil, err
	}
	for _, it := range ex.items {
		if err := put(it.Path, it.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish the bundle: %w", err)
	}
	return buf.Bytes(), nil
}

// collectConnections gathers the definitions the selection asks for, each
// sanitised. A definition that cannot be made portable is left out, with a
// warning in the manifest.
func collectConnections(t Target, sel Selection) ([]workspace.ConnectionDef, []string, error) {
	c := &connCollector{have: map[string]bool{}}
	if sel.SharedConnections || len(sel.TeamConnections) > 0 {
		if err := c.team(t, sel); err != nil {
			return nil, nil, err
		}
	}
	if len(sel.LocalConnections) > 0 {
		if err := c.local(t, sel.LocalConnections); err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(c.out, func(i, j int) bool { return c.out[i].Name < c.out[j].Name })
	return c.out, c.warnings, nil
}

// connCollector accumulates sanitised connection definitions.
type connCollector struct {
	out      []workspace.ConnectionDef
	warnings []string
	have     map[string]bool
}

func (c *connCollector) take(d workspace.ConnectionDef, where string) {
	clean, notes, err := SanitizeConnection(d)
	c.warnings = append(c.warnings, notes...)
	if err != nil {
		c.warnings = append(c.warnings, fmt.Sprintf("connection %q (%s) was left out: %v", d.Name, where, err))
		return
	}
	if c.have[clean.Name] {
		c.warnings = append(c.warnings, fmt.Sprintf("connection %q (%s) was left out: the name is already taken by the team definition", d.Name, where))
		return
	}
	c.have[clean.Name] = true
	c.out = append(c.out, clean)
}

// team takes the team's vibe.yaml definitions: all of them, or the named ones.
func (c *connCollector) team(t Target, sel Selection) error {
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
			c.take(d, "team")
		}
	}
	for n := range want {
		if !found[n] {
			return fmt.Errorf("there is no team connection named %q", n)
		}
	}
	return nil
}

// local takes the named "This computer" definitions.
func (c *connCollector) local(t Target, names []string) error {
	local := map[string]workspace.ConnectionDef{}
	for _, d := range t.Local().Connections {
		local[d.Name] = d
	}
	for _, name := range names {
		d, ok := local[name]
		if !ok {
			return fmt.Errorf("there is no \"This computer\" connection named %q", name)
		}
		c.take(d, "this computer")
	}
	return nil
}
