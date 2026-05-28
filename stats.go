package pixelize

import (
	"encoding/json"
	"io"
	"sort"
)

// Bucket is one entry of a histogram.
type Bucket[M any] struct {
	Index int      `json:"index"`
	Entry Entry[M] `json:"entry"`
	Count int      `json:"count"`
}

// UniqueColors returns the number of palette entries actually used.
func (p *Pattern[M]) UniqueColors() int {
	return len(p.Usage)
}

// Histogram returns palette indices sorted by usage count (descending).
func (p *Pattern[M]) Histogram() []Bucket[M] {
	out := make([]Bucket[M], 0, len(p.Usage))
	for idx, count := range p.Usage {
		out = append(out, Bucket[M]{
			Index: idx,
			Entry: p.Palette[idx],
			Count: count,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// Dominant returns the top n entries by usage count.
func (p *Pattern[M]) Dominant(n int) []Entry[M] {
	h := p.Histogram()
	if n > len(h) {
		n = len(h)
	}
	out := make([]Entry[M], n)
	for i := 0; i < n; i++ {
		out[i] = h[i].Entry
	}
	return out
}

// StatsJSON is the machine-readable stats summary.
type StatsJSON[M any] struct {
	Width        int         `json:"width"`
	Height       int         `json:"height"`
	TotalPixels  int         `json:"total_pixels"`
	UniqueColors int         `json:"unique_colors"`
	PaletteSize  int         `json:"palette_size"`
	Histogram    []Bucket[M] `json:"histogram"`
}

// Stats builds the JSON-friendly summary for a Pattern.
func (p *Pattern[M]) Stats() StatsJSON[M] {
	b := p.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	return StatsJSON[M]{
		Width:        w,
		Height:       h,
		TotalPixels:  w * h,
		UniqueColors: p.UniqueColors(),
		PaletteSize:  len(p.Palette),
		Histogram:    p.Histogram(),
	}
}

// WriteStatsJSON marshals the stats summary to w.
func (p *Pattern[M]) WriteStatsJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p.Stats())
}
