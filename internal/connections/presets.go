package connections

import (
	"fmt"
	"strings"
)

// RegionPlaceholder stands for the Region field inside a preset endpoint
// ("s3.<region>.backblazeb2.com"). It is filled from the region before the
// endpoint is used, and a definition that still holds it is refused by the
// endpoint rule (< and > are not host characters).
const RegionPlaceholder = "<region>"

// Preset fills the s3 form for a known S3-compatible store. It sets only the
// three storage options; nothing of it is saved in the definition, so a preset
// adds nothing to the fingerprint or to what a person's credential applies to.
type Preset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Options are the option values the preset sets (endpoint, url_style,
	// use_ssl). An empty map (AWS, Other) sets none.
	Options map[string]string `json:"options"`
	// RegionHint is a suggested region ("" for none).
	RegionHint string `json:"regionHint,omitempty"`
	Help       string `json:"help,omitempty"`
}

var s3Presets = []Preset{
	{ID: "aws", Label: "Amazon S3", Options: map[string]string{}, Help: "Amazon's own S3: no endpoint needed."},
	{ID: "minio", Label: "MinIO", Options: map[string]string{"endpoint": "localhost:9000", "url_style": "path", "use_ssl": "false"},
		Help: "A MinIO server on this computer. Change the endpoint for another host and set TLS on."},
	{ID: "b2", Label: "Backblaze B2", Options: map[string]string{"endpoint": "s3." + RegionPlaceholder + ".backblazeb2.com", "url_style": "vhost", "use_ssl": "true"},
		RegionHint: "us-west-004", Help: "Use the region of your bucket, the part of its S3 endpoint after \"s3.\"."},
	{ID: "floci", Label: "Floci", Options: map[string]string{"endpoint": "localhost:4566", "url_style": "path", "use_ssl": "false"},
		Help: "The Floci local emulator on its default port. Change the endpoint if you moved it, and set TLS on for a remote host."},
	{ID: "other", Label: "Other S3-compatible", Options: map[string]string{}, Help: "Fill in the endpoint your provider gives you."},
}

// storagePresets returns the presets of a storage type (s3 only), as a copy.
func storagePresets(kind string) []Preset {
	if kind != "s3" {
		return nil
	}
	out := make([]Preset, len(s3Presets))
	for i, p := range s3Presets {
		p.Options = copyMap(p.Options)
		out[i] = p
	}
	return out
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// PresetIDs lists the s3 preset ids, for help text and error messages.
func PresetIDs() []string {
	ids := make([]string, len(s3Presets))
	for i, p := range s3Presets {
		ids[i] = p.ID
	}
	return ids
}

// PresetOptions returns the options of the s3 preset id with the region
// placeholder filled from region. A preset whose endpoint needs a region
// (B2) fails when region is empty, or is not a region name, so the endpoint
// can never carry anything but a region in that position. An unknown id is an
// error that lists the ids.
func PresetOptions(id, region string) (map[string]string, error) {
	for _, p := range s3Presets {
		if p.ID != id {
			continue
		}
		out := copyMap(p.Options)
		if ep := out["endpoint"]; strings.Contains(ep, RegionPlaceholder) {
			if region == "" {
				return nil, fmt.Errorf("preset %s needs a region (--region), for example %s", id, p.RegionHint)
			}
			if !regionRE.MatchString(region) {
				return nil, fmt.Errorf("region %q is not a region name", region)
			}
			out["endpoint"] = strings.ReplaceAll(ep, RegionPlaceholder, region)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown preset %q (want one of %s)", id, strings.Join(PresetIDs(), ", "))
}
