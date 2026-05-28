package pixelize

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Named is implemented by metadata types that carry a human-readable
// name. EntryMeta satisfies this.
type Named interface {
	GetName() string
}

// Identified is implemented by metadata types that carry an ID
// (legoid, brick-link sku, dmc thread number, ...).
type Identified interface {
	GetID() string
}

// Hexed exposes the canonical hex string.
type Hexed interface {
	GetHex() string
}

// GetName satisfies Named.
func (m EntryMeta) GetName() string { return m.Name }

// GetID satisfies Identified.
func (m EntryMeta) GetID() string { return m.ID }

// GetHex satisfies Hexed.
func (m EntryMeta) GetHex() string { return m.Hex }

// WriteBuildMap writes a per-pixel build map suitable for assembling
// physical mosaics. One line per pixel:
//
//	[x][y] = R:r, G:g, B:b   -name
//
// Works with any Pattern whose metadata implements Named (else the
// name field is left blank).
func WriteBuildMap[M any](w io.Writer, p *Pattern[M]) error {
	bw := newWriter(w)
	defer bw.Flush()

	for x := 0; x < len(p.Indices); x++ {
		col := p.Indices[x]
		for y := 0; y < len(col); y++ {
			e := p.Palette[col[y]]
			name := nameOf(e.Meta)
			if _, err := fmt.Fprintf(bw, "[%d][%d] = R:%d, G:%d, B:%d\t-%s\n",
				x, y, e.R, e.G, e.B, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// WritePiecesCSV writes a pieces summary as CSV, sorted by count desc.
// Columns: id, name, hex, count. Empty cells when metadata fields are
// absent.
func WritePiecesCSV[M any](w io.Writer, p *Pattern[M]) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{"id", "name", "hex", "count"}); err != nil {
		return err
	}

	type row struct {
		id, name, hex string
		count         int
	}
	rows := make([]row, 0, len(p.Usage))
	for idx, count := range p.Usage {
		e := p.Palette[idx]
		rows = append(rows, row{
			id:    idOf(e.Meta),
			name:  nameOf(e.Meta),
			hex:   hexOf(e.Meta, e.R, e.G, e.B),
			count: count,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].name < rows[j].name
	})

	for _, r := range rows {
		if err := cw.Write([]string{
			r.id, r.name, r.hex, fmt.Sprintf("%d", r.count),
		}); err != nil {
			return err
		}
	}
	return nil
}

// flushWriter abstracts the bufio.Writer-style Flush so callers don't
// need to know whether they got an already-buffered Writer or not.
type flushWriter struct {
	w  io.Writer
	bw interface{ Flush() error }
}

func (f *flushWriter) Write(p []byte) (int, error) { return f.w.Write(p) }
func (f *flushWriter) Flush() error {
	if f.bw == nil {
		return nil
	}
	return f.bw.Flush()
}

func newWriter(w io.Writer) *flushWriter {
	return &flushWriter{w: w}
}

func nameOf(meta any) string {
	if v, ok := meta.(Named); ok {
		return v.GetName()
	}
	if s, ok := meta.(string); ok {
		return s
	}
	if s, ok := meta.(fmt.Stringer); ok {
		return s.String()
	}
	return ""
}

func idOf(meta any) string {
	if v, ok := meta.(Identified); ok {
		return v.GetID()
	}
	return ""
}

func hexOf(meta any, r, g, b uint8) string {
	if v, ok := meta.(Hexed); ok {
		if h := v.GetHex(); h != "" {
			return strings.ToUpper(strings.TrimPrefix(h, "#"))
		}
	}
	return fmt.Sprintf("%02X%02X%02X", r, g, b)
}
