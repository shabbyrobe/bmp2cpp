package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"

	"github.com/shabbyrobe/wu2quant"
	"golang.org/x/image/draw"
)

type Generator struct {
	Palette       Palette `json:"palette,omitempty"`
	Invert        bool    `json:"invert,omitempty"`
	TargetWidth   int     `json:"targetWidth,omitempty"`
	TargetHeight  int     `json:"targetHeight,omitempty"`
	Scaler        string  `json:"scaler,omitempty"`
	Renderer      string  `json:"renderer,omitempty"`
	VarName       string  `json:"varName,omitempty"`
	PaletteOffset int     `json:"paletteOffset,omitempty"`
	RowWiseJS     bool    `json:"rowWiseJS,omitempty"`
}

func (g *Generator) Clone() *Generator {
	clone := *g
	return &clone
}

func (g *Generator) Build(img image.Image) (string, error) {
	// Rescale:
	if g.TargetWidth > 0 || g.TargetHeight > 0 {
		newSize := prepareSize(g.TargetWidth, g.TargetHeight, img.Bounds().Size())
		nb := image.Rectangle{Max: newSize}
		dst := image.NewRGBA(nb)
		scl := findScaler(g.Scaler)
		scl.Scale(dst, nb, img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	// Quantise:
	quant := wu2quant.New()
	palimg, err := quant.ToPaletted(g.Palette.Size, img, nil)
	if err != nil {
		return "", err
	}

	// Sort colors by intensity (HSP colour space):
	paletteIndexes := uniquePaletteIndexes(palimg)
	sort.Slice(paletteIndexes, func(i, j int) bool {
		if g.Invert {
			return hsp(palimg.Palette[paletteIndexes[i]]) > hsp(palimg.Palette[paletteIndexes[j]])
		} else {
			return hsp(palimg.Palette[paletteIndexes[i]]) < hsp(palimg.Palette[paletteIndexes[j]])
		}
	})

	// PaletteIndexes should now be sorted by HSP intensity, so the index will be our
	// intensity ordering. Map the unique, sorted colors back to the palette characters,
	// which are ordered by intensity too:
	paletteIndexToChar := [256]rune{}
	for intensity, v := range paletteIndexes {
		paletteIndexToChar[v] = g.Palette.IntensityRune[intensity]
	}

	var b bytes.Buffer
	png.Encode(&b, palimg)
	os.WriteFile("/tmp/s.png", b.Bytes(), 0600)

	var out bytes.Buffer
	var renderCtx = &renderContext{
		paletteIndexes,
		paletteIndexToChar,
		g,
		palimg,
	}
	if err := render(g, renderCtx, &out); err != nil {
		return "", err
	}

	return out.String(), nil
}

func hsp(col color.Color) float64 {
	r, g, b, _ := col.RGBA()

	rf := float64(r) / 0xffff
	gf := float64(g) / 0xffff
	bf := float64(b) / 0xffff

	rfs := 0.299 * rf
	gfs := 0.587 * gf
	bfs := 0.114 * bf

	return math.Sqrt(rfs*rfs + gfs*gfs + bfs*bfs)
}

func mapSeenChars(img *image.Paletted, paletteIndexToChar [256]rune) map[rune]bool {
	out := map[rune]bool{}
	width := img.Bounds().Dx()
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < width; x++ {
			px := img.ColorIndexAt(x, y)
			char := paletteIndexToChar[px]
			out[char] = true
		}
	}
	return out
}

func uniquePaletteIndexes(palimg *image.Paletted) []uint8 {
	foundColors := [256]bool{}
	width := palimg.Bounds().Dx()
	for y := 0; y < palimg.Bounds().Dy(); y++ {
		for x := 0; x < width; x++ {
			px := palimg.ColorIndexAt(x, y)
			foundColors[px] = true
		}
	}
	colors := make([]uint8, 0)
	for fc, ok := range foundColors {
		if ok {
			colors = append(colors, uint8(fc))
		}
	}
	return colors
}

func subImage(img image.Image, r image.Rectangle) image.Image {
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	sub := img.(subImager).SubImage(r)
	return sub
}

func prepareSize(targetWidth, targetHeight int, orig image.Point) image.Point {
	if targetWidth <= 0 {
		targetWidth = int(
			math.Round(float64(orig.X) * (float64(targetHeight) / float64(orig.Y))))
	}
	if targetHeight <= 0 {
		targetHeight = int(
			math.Round(float64(orig.Y) * (float64(targetWidth) / float64(orig.X))))
	}
	return image.Point{targetWidth, targetHeight}
}
