package pixelize

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EntryMeta is the metadata struct used by the CLI loaders. It is
// generic-friendly: any field can be empty.
type EntryMeta struct {
	ID   string            `json:"id,omitempty"`
	Name string            `json:"name,omitempty"`
	Hex  string            `json:"hex,omitempty"`
	More map[string]string `json:"more,omitempty"`
}

// LoadFile dispatches to the right loader based on file extension.
// Supports .csv, .hex, .gpl, .json.
func LoadFile(path string) (Palette[EntryMeta], error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".csv":
		return LoadCSV(f)
	case ".hex":
		return LoadHEX(f)
	case ".gpl":
		return LoadGPL(f)
	case ".json":
		return LoadJSON(f)
	default:
		return nil, fmt.Errorf("unsupported palette extension %q (want .csv, .hex, .gpl, .json)", ext)
	}
}

// LoadCSV reads a header-driven CSV palette. Recognized columns:
// hex, name, id, r, g, b. Order is free. Either "hex" or all of
// "r","g","b" must be present.
//
// No comment syntax: "#" is reserved for hex literals like "#FF0000".
// Use HEX format if you need inline comments.
func LoadCSV(r io.Reader) (Palette[EntryMeta], error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv parse: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows")
	}

	header := records[0]
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	hasHex := has(idx, "hex")
	hasRGB := has(idx, "r") && has(idx, "g") && has(idx, "b")
	if !hasHex && !hasRGB {
		return nil, fmt.Errorf("csv must have 'hex' column or 'r','g','b' columns")
	}

	out := make(Palette[EntryMeta], 0, len(records)-1)
	for ln, row := range records[1:] {
		var r8, g8, b8 uint8
		var hexStr string

		if hasRGB {
			r, err1 := strconv.Atoi(strings.TrimSpace(row[idx["r"]]))
			g, err2 := strconv.Atoi(strings.TrimSpace(row[idx["g"]]))
			b, err3 := strconv.Atoi(strings.TrimSpace(row[idx["b"]]))
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, fmt.Errorf("row %d: invalid r,g,b", ln+2)
			}
			r8, g8, b8 = clamp8(r), clamp8(g), clamp8(b)
			if hasHex {
				hexStr = strings.TrimSpace(row[idx["hex"]])
			} else {
				hexStr = fmt.Sprintf("%02X%02X%02X", r8, g8, b8)
			}
		} else {
			hexStr = strings.TrimSpace(row[idx["hex"]])
			r8, g8, b8, err = parseHex(hexStr)
			if err != nil {
				return nil, fmt.Errorf("row %d: hex %q: %w", ln+2, hexStr, err)
			}
		}

		meta := EntryMeta{Hex: strings.TrimPrefix(strings.ToUpper(hexStr), "#")}
		if i, ok := idx["id"]; ok && i < len(row) {
			meta.ID = strings.TrimSpace(row[i])
		}
		if i, ok := idx["name"]; ok && i < len(row) {
			meta.Name = strings.TrimSpace(row[i])
		}
		// Capture any extra columns under More.
		for col, ci := range idx {
			if isKnownCol(col) {
				continue
			}
			if ci >= len(row) {
				continue
			}
			if meta.More == nil {
				meta.More = map[string]string{}
			}
			meta.More[col] = strings.TrimSpace(row[ci])
		}

		out = append(out, Entry[EntryMeta]{R: r8, G: g8, B: b8, Meta: meta})
	}
	return out, nil
}

// LoadHEX reads one #rrggbb (or rrggbb) per line. Comments start with #
// in column zero or after whitespace; a hex line may not start with #
// the same way, but we tell them apart by length: 6 or 7 chars => color.
func LoadHEX(r io.Reader) (Palette[EntryMeta], error) {
	scanner := bufio.NewScanner(r)
	var out Palette[EntryMeta]
	n := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// A standalone "# comment ..." line: skip.
		if strings.HasPrefix(line, "#") && len(line) != 7 {
			continue
		}
		r8, g8, b8, err := parseHex(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		hexStr := strings.TrimPrefix(strings.ToUpper(line), "#")
		out = append(out, Entry[EntryMeta]{
			R: r8, G: g8, B: b8,
			Meta: EntryMeta{Hex: hexStr, Name: fmt.Sprintf("color_%d", n+1)},
		})
		n++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no colors in HEX palette")
	}
	return out, nil
}

// LoadGPL reads a GIMP palette. Format:
//
//	GIMP Palette
//	Name: ...
//	Columns: N
//	#
//	R   G   B   Name
//	...
func LoadGPL(r io.Reader) (Palette[EntryMeta], error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return nil, fmt.Errorf("empty GPL")
	}
	if !strings.HasPrefix(scanner.Text(), "GIMP Palette") {
		return nil, fmt.Errorf("missing 'GIMP Palette' header")
	}
	var out Palette[EntryMeta]
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip "Name:" and "Columns:" header lines.
		if strings.HasPrefix(line, "Name:") || strings.HasPrefix(line, "Columns:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		r, err1 := strconv.Atoi(fields[0])
		g, err2 := strconv.Atoi(fields[1])
		b, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		name := ""
		if len(fields) >= 4 {
			name = strings.Join(fields[3:], " ")
		}
		r8, g8, b8 := clamp8(r), clamp8(g), clamp8(b)
		out = append(out, Entry[EntryMeta]{
			R: r8, G: g8, B: b8,
			Meta: EntryMeta{
				Name: name,
				Hex:  fmt.Sprintf("%02X%02X%02X", r8, g8, b8),
			},
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no colors in GPL palette")
	}
	return out, nil
}

// jsonPaletteFile mirrors the documented JSON format.
type jsonPaletteFile struct {
	Name    string             `json:"name"`
	Entries []jsonPaletteEntry `json:"entries"`
}

type jsonPaletteEntry struct {
	R    uint8             `json:"r"`
	G    uint8             `json:"g"`
	B    uint8             `json:"b"`
	Hex  string            `json:"hex,omitempty"`
	Name string            `json:"name,omitempty"`
	ID   string            `json:"id,omitempty"`
	Meta map[string]string `json:"meta,omitempty"`
}

// LoadJSON reads the structured JSON palette format documented in palettes/README.md.
func LoadJSON(r io.Reader) (Palette[EntryMeta], error) {
	var jf jsonPaletteFile
	if err := json.NewDecoder(r).Decode(&jf); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}
	out := make(Palette[EntryMeta], 0, len(jf.Entries))
	for i, e := range jf.Entries {
		hex := e.Hex
		if hex == "" {
			hex = fmt.Sprintf("%02X%02X%02X", e.R, e.G, e.B)
		}
		out = append(out, Entry[EntryMeta]{
			R: e.R, G: e.G, B: e.B,
			Meta: EntryMeta{
				ID:   e.ID,
				Name: e.Name,
				Hex:  strings.TrimPrefix(strings.ToUpper(hex), "#"),
				More: e.Meta,
			},
		})
		_ = i
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no entries in JSON palette")
	}
	return out, nil
}

func parseHex(s string) (uint8, uint8, uint8, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, fmt.Errorf("hex must be 6 chars, got %q", s)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("hex parse: %w", err)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func has(m map[string]int, k string) bool {
	_, ok := m[k]
	return ok
}

func isKnownCol(c string) bool {
	switch c {
	case "hex", "name", "id", "r", "g", "b":
		return true
	}
	return false
}
