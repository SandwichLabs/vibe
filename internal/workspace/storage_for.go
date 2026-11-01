package workspace

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/SandwichLabs/slaw/internal/connections"
	"github.com/SandwichLabs/slaw/internal/storageurl"
)

// StorageMatch is the storage connection whose scope holds a URL.
type StorageMatch struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Scope string `json:"scope"`
	// In is the workspace scope (user, shared, local) the connection comes from.
	In Scope `json:"in"`
}

// StorageSuggestion is the storage connection to add when none covers a URL.
type StorageSuggestion struct {
	Type string `json:"type"`
	// Scope is the common folder of the uncovered URLs in one bucket.
	Scope string `json:"scope"`
	// BucketScope is the whole bucket, the wider choice.
	BucketScope string `json:"bucket_scope"`
	Name        string `json:"name"`
}

// StorageNeed is the answer for one storage URL of a lake or table definition:
// the connection that covers it, or what to add, or why it cannot be covered.
type StorageNeed struct {
	URL string `json:"url"`
	// What names the part of the definition the URL is ("catalog", "data path",
	// "location"); empty when the caller gave plain URLs.
	What    string             `json:"what,omitempty"`
	Match   *StorageMatch      `json:"match,omitempty"`
	Suggest *StorageSuggestion `json:"suggest,omitempty"`
	Problem string             `json:"problem,omitempty"`
	// Group numbers (from 1) the uncovered URLs that share one Suggest, so a
	// caller prints one command per group; 0 for a need with no suggestion.
	Group int `json:"group,omitempty"`
	// Templated is true when the definition wrote the URL with ${ENV}
	// references. URL is then the text as typed (never the expanded value) and
	// Suggest carries no scope, only the type and name.
	Templated bool `json:"templated,omitempty"`
}

// storageTarget is one URL to check with its label.
type storageTarget struct{ url, what string }

// storageTargets are the URLs of a resolved definition that must lie inside a
// storage connection's scope: a lake's catalog and data path, a table's
// location. It is the one list both the open-time check and StorageFor read.
func storageTargets(d ConnectionDef, path string) []storageTarget {
	var targets []storageTarget
	switch d.Type {
	case "ducklake":
		targets = []storageTarget{{path, "catalog"}, {d.Options["data_path"], "data path"}}
	case "iceberg", "delta":
		targets = []storageTarget{{path, "location"}}
	}
	return targets
}

// bestStorageMatch returns the storage connection of ws whose scope holds url:
// the most specific scope wins, as for the secret DuckDB picks (longest
// matching prefix); a tie goes to the first by name. self (the lake being
// checked) is skipped.
func bestStorageMatch(ws Workspace, self, url string, lookup LookupFunc) (StorageMatch, bool) {
	var best StorageMatch
	bestLen := -1
	for _, c := range ws.Connections() {
		if !storageTypes[c.Type] || c.Name == self {
			continue
		}
		sd, err := c.ConnectionDef.Resolve(lookup, c.Scope)
		if err != nil || sd.Scope == "" {
			continue
		}
		// One normaliser, shared with the agent's URL rule: a literal prefix
		// match on the scope's folder form.
		if storageurl.Within(url, sd.Scope) && len(sd.Scope) > bestLen {
			best, bestLen = StorageMatch{Name: c.Name, Type: c.Type, Scope: c.ConnectionDef.Scope, In: c.Scope}, len(sd.Scope)
		}
	}
	return best, bestLen >= 0
}

// StorageFor says, for each storage URL, which connection of ws covers it, or
// what connection to add. It uses the same match rule as the open-time check
// (checkStorageURLs); the save path is unchanged, a lake may still be saved
// before its storage connection exists.
func StorageFor(ws Workspace, urls []string, lookup LookupFunc) []StorageNeed {
	targets := make([]storageTarget, len(urls))
	for i, u := range urls {
		targets[i] = storageTarget{url: u}
	}
	return storageForTargets(ws, "", targets, lookup)
}

// StorageForConnection is StorageFor for the storage URLs of one saved lake or
// table, labelled by what they are. ${ENV} references are expanded to decide
// the match, but the result never carries an expanded value (see
// StorageNeed.Templated). A definition with no storage URL gives no needs.
func StorageForConnection(ws Workspace, c Connection, lookup LookupFunc) []StorageNeed {
	typed := storageTargets(c.ConnectionDef, c.Path)
	resolved, err := c.ConnectionDef.Resolve(lookup, c.Scope)
	var expanded []storageTarget
	if err == nil {
		expanded = storageTargets(resolved, resolved.Path)
	}
	var (
		urls, whats, typedBy []string
		failed               []StorageNeed
	)
	for i, t := range typed {
		if t.url == "" || !connections.IsDataURL(t.url) {
			continue
		}
		if err != nil && strings.Contains(t.url, "${") { // an unset variable: hint on the typed value
			failed = append(failed, StorageNeed{URL: t.url, What: t.what, Templated: true})
			continue
		}
		u := t.url
		if err == nil {
			u = expanded[i].url
		}
		urls, whats, typedBy = append(urls, u), append(whats, t.what), append(typedBy, t.url)
	}
	needs := StorageFor(ws, urls, lookup)
	for i := range needs {
		needs[i].What = whats[i]
	}
	return hideExpanded(ws, needs, typedBy, failed)
}

// hideExpanded rewrites the needs of a definition written with ${ENV}
// references so that nothing expanded remains: the URL as typed, no scope in
// a suggestion, a name made from the type alone, and no quoted problem. A
// group that holds any templated URL is hidden whole (its common folder would
// otherwise reveal the expansion), and each hidden group gets its own name.
func hideExpanded(ws Workspace, needs []StorageNeed, typed []string, failed []StorageNeed) []StorageNeed {
	hiddenGroup := map[int]bool{}
	maxGroup := 0
	for i, n := range needs {
		maxGroup = max(maxGroup, n.Group)
		if strings.Contains(typed[i], "${") && n.Group > 0 {
			hiddenGroup[n.Group] = true
		}
	}
	taken := existingNames(ws)
	names := map[int]string{}
	nameFor := func(group int, typ string) string {
		if group > 0 && names[group] != "" {
			return names[group]
		}
		name := suggestName(typ, "", taken)
		taken = append(taken, name)
		if group > 0 {
			names[group] = name
		}
		return name
	}
	for i := range needs {
		n := &needs[i]
		n.URL = typed[i]
		isTemplated := strings.Contains(typed[i], "${")
		n.Templated = isTemplated
		if n.Suggest != nil && hiddenGroup[n.Group] {
			n.Suggest = &StorageSuggestion{Type: n.Suggest.Type, Name: nameFor(n.Group, n.Suggest.Type)}
		}
		if isTemplated && n.Problem != "" { // a problem message quotes the expanded value
			n.Problem = "The URL is not a plain storage URL once its ${...} references are filled in"
		}
	}
	for i := range failed {
		if typ, ok := connections.StorageTypeForScheme(storageurl.Scheme(failed[i].URL)); ok {
			maxGroup++
			failed[i].Group = maxGroup
			failed[i].Suggest = &StorageSuggestion{Type: typ, Name: nameFor(0, typ)}
		}
	}
	return append(needs, failed...)
}

// storageForTargets is the shared body of StorageFor and StorageForConnection.
func storageForTargets(ws Workspace, self string, targets []storageTarget, lookup LookupFunc) []StorageNeed {
	needs := make([]StorageNeed, len(targets))
	// groups collects the uncovered URLs by bucket, in first-seen order.
	var order []string
	groups := map[string][]int{}
	for i, t := range targets {
		needs[i] = StorageNeed{URL: t.url, What: t.what}
		if p := storageProblem(t.url); p != "" {
			needs[i].Problem = p
			continue
		}
		if m, ok := bestStorageMatch(ws, self, t.url, lookup); ok {
			needs[i].Match = &m
			continue
		}
		b, err := storageurl.Bucket(t.url)
		if err != nil {
			needs[i].Problem = notPlain(targets[i].url, err)
			continue
		}
		if b == t.url+"/" { // after the match: a scope may name the bare bucket (az://host/)
			needs[i].Problem = "Write the bucket with a trailing slash (" + b + ") or name a file or folder inside it"
			continue
		}
		if _, ok := groups[b]; !ok {
			order = append(order, b)
		}
		groups[b] = append(groups[b], i)
	}
	taken := existingNames(ws)
	for g, b := range order {
		idx := groups[b]
		sug, err := suggestFor(targets, idx, b, taken)
		if err != nil {
			for _, i := range idx {
				needs[i].Problem = notPlain(targets[i].url, err)
			}
			continue
		}
		taken = append(taken, sug.Name)
		for _, i := range idx {
			needs[i].Suggest, needs[i].Group = sug, g+1
		}
	}
	return needs
}

// storageProblem says why a URL cannot be covered by a storage connection at
// all ("" when it can).
func storageProblem(u string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return "Read it through an S3 connection with an endpoint"
	}
	if err := storageurl.Validate(u); err != nil {
		return notPlain(u, err)
	}
	return ""
}

// notPlain is the problem text for a URL that is not a plain storage URL. The
// reason is the validator's, with the URL it quotes replaced by "the URL": the
// text is shown next to the field the URL was typed into, and a templated or
// pasted value is never echoed back.
func notPlain(u string, err error) string {
	return "Not a plain storage URL: " + strings.ReplaceAll(err.Error(), strconv.Quote(u), "the URL")
}

// suggestFor builds the one suggestion for the uncovered URLs idx, which all
// lie in the bucket b.
func suggestFor(targets []storageTarget, idx []int, bucket string, taken []string) (*StorageSuggestion, error) {
	urls := make([]string, len(idx))
	for k, i := range idx {
		urls[k] = targets[i].url
	}
	folder, err := storageurl.CommonFolder(urls)
	if err != nil {
		return nil, err
	}
	typ, ok := connections.StorageTypeForScheme(storageurl.Scheme(bucket))
	if !ok {
		return nil, fmt.Errorf("%q is not an object-storage URL", urls[0])
	}
	bn, err := storageurl.BucketName(urls[0])
	if err != nil {
		return nil, err
	}
	return &StorageSuggestion{Type: typ, Scope: folder, BucketScope: bucket, Name: suggestName(typ, bn, taken)}, nil
}

func existingNames(ws Workspace) []string {
	if ws == nil {
		return slices.Clone(ReservedNames)
	}
	names := slices.Clone(ReservedNames)
	for _, c := range ws.Connections() {
		names = append(names, c.Name)
	}
	return names
}

// suggestName makes a connection name from a bucket: lower case, [a-z0-9_]
// only, "<bucket>_<type>" (a leading digit, or nothing usable left, puts the
// type first), de-duplicated with _2, _3 and never one of taken.
func suggestName(typ, bucket string, taken []string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(bucket) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	base := strings.Trim(b.String(), "_")
	switch {
	case base == "":
		base = typ
	case base[0] >= '0' && base[0] <= '9':
		base = typ + "_" + base
	default:
		base += "_" + typ
	}
	if len(base) > 55 {
		base = strings.TrimRight(base[:55], "_")
	}
	name := base
	for n := 2; slices.Contains(taken, name) || ValidateConnectionName(name) != nil; n++ {
		name = fmt.Sprintf("%s_%d", base, n)
		if n > 1000 {
			return typ + "_storage"
		}
	}
	return name
}
