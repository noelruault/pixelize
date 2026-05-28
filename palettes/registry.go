package palettes

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SupportedExts are the palette file extensions resolvable by name.
var SupportedExts = []string{".csv", ".hex", ".gpl", ".json"}

// UserDir returns $XDG_CONFIG_HOME/pixelize/palettes (or
// ~/.config/pixelize/palettes when XDG_CONFIG_HOME is unset).
func UserDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pixelize", "palettes"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pixelize", "palettes"), nil
}

// Source labels where a resolved palette was found.
type Source string

const (
	SourceUser     Source = "user"
	SourceEmbedded Source = "embedded"
)

// Resolved describes a successfully resolved palette name.
type Resolved struct {
	Name   string
	Path   string // filesystem path (user dir) or fs path (embedded)
	Ext    string // ".csv" etc.
	Source Source
	Reader io.ReadCloser
}

// Resolve looks up a palette by short name. User-dir matches win over
// embedded matches. Returns ErrNotFound if no match is found.
func Resolve(name string) (*Resolved, error) {
	udir, err := UserDir()
	if err == nil {
		for _, ext := range SupportedExts {
			p := filepath.Join(udir, name+ext)
			if f, err := os.Open(p); err == nil {
				return &Resolved{
					Name: name, Path: p, Ext: ext,
					Source: SourceUser, Reader: f,
				}, nil
			}
		}
	}

	for _, ext := range SupportedExts {
		p := name + ext
		f, err := Embedded.Open(p)
		if err != nil {
			continue
		}
		return &Resolved{
			Name: name, Path: p, Ext: ext,
			Source: SourceEmbedded, Reader: f,
		}, nil
	}

	return nil, fmt.Errorf("palette %q not found (looked in %s and embedded)", name, udir)
}

// List returns the names of all resolvable palettes from both sources.
// User-dir names shadow embedded ones (no duplicates in output).
type Listing struct {
	Name   string
	Source Source
}

func List() ([]Listing, error) {
	seen := map[string]Source{}

	if udir, err := UserDir(); err == nil {
		if entries, err := os.ReadDir(udir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if !isSupportedExt(ext) {
					continue
				}
				n := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
				seen[n] = SourceUser
			}
		}
	}

	if err := fs.WalkDir(Embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !isSupportedExt(ext) {
			return nil
		}
		n := strings.TrimSuffix(p, filepath.Ext(p))
		if _, ok := seen[n]; !ok {
			seen[n] = SourceEmbedded
		}
		return nil
	}); err != nil {
		return nil, err
	}

	out := make([]Listing, 0, len(seen))
	for n, src := range seen {
		out = append(out, Listing{Name: n, Source: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Init copies all embedded palettes into the user dir. Skips files
// that already exist (does not overwrite).
type InitResult struct {
	Dir      string
	Copied   []string
	Skipped  []string // already existed
}

func Init() (*InitResult, error) {
	udir, err := UserDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(udir, 0o755); err != nil {
		return nil, err
	}

	res := &InitResult{Dir: udir}

	entries, err := fs.ReadDir(Embedded, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(udir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			res.Skipped = append(res.Skipped, e.Name())
			continue
		}
		data, err := Embedded.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
		res.Copied = append(res.Copied, e.Name())
	}
	sort.Strings(res.Copied)
	sort.Strings(res.Skipped)
	return res, nil
}

func isSupportedExt(ext string) bool {
	for _, e := range SupportedExts {
		if e == ext {
			return true
		}
	}
	return false
}
