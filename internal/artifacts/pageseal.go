package artifacts

import "path"

// PageFromText builds the artifact a renderer or a host works from out of the
// bytes that were read once: artifact.yaml's text and the html/ file set. It
// decodes and vets exactly as the stores do (a refused page is an error) and
// seals the hash over those same bytes, so a host that compares Page.Hash with
// the allowed hash renders what was hashed. name is only a label.
func PageFromText(name string, manifestText []byte, files []PageFile) (*Artifact, error) {
	m, err := Decode(manifestText)
	if err != nil {
		return nil, err
	}
	if m.View != ViewHTML {
		return nil, invalid("the manifest is not a view: %s", ViewHTML)
	}
	files = sortedFiles(files)
	page, err := VetPage(m, files)
	if err != nil {
		return nil, err
	}
	if err := sealPage(manifestText, files, page); err != nil {
		return nil, err
	}
	return &Artifact{Name: name, Manifest: *m, Files: files, Page: page}, nil
}

// PageFileRole is the role a page file has in the hash and on the allow
// screen: entry, partial, script, style, image, font or data.
func PageFileRole(p, entry string) string { return pageRole(p, entry) }

// IsTextPageFile reports whether a page file is text that can be shown as
// source (templates, scripts, stylesheets, SVG and JSON data); the others are
// binary assets.
func IsTextPageFile(p string) bool {
	switch path.Ext(p) {
	case ".tmpl", ".js", ".mjs", ".css", ".svg", ".json":
		return true
	}
	return false
}
