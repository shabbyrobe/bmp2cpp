package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/shabbyrobe/wu2quant"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var sizeRaw string
	var mapFile string
	var gen Generator

	flags := flag.NewFlagSet("", 0)
	flags.StringVar(&sizeRaw, "size", "", "Size, in '<w>x<h>' format. <=0 for either dimension for aspect.")
	flags.StringVar(&gen.PaletteChars, "chars", "_cowgCONW", "Palette chars, ordered from least to most intense. Must be valid identifier characters.")
	flags.StringVar(&gen.Scaler, "scaler", "catmullrom", "Scaler when resizing. Values: nn, approxbilinear, bilinear, catmullrom.")
	flags.StringVar(&gen.Renderer, "renderer", "cpp17", "Renderer. Values: cpp17, cpp, js.")
	flags.StringVar(&gen.VarName, "var", "bitmap", "Output variable name.")
	flags.BoolVar(&gen.Invert, "invert", false, "Invert colours")
	flags.StringVar(&mapFile, "map", "", "Image map file (defines regions")
	flags.IntVar(&gen.PaletteOffset, "offset", 0, "Palette offset")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	if len(sizeRaw) > 0 {
		if _, err := fmt.Sscanf(sizeRaw, "%dx%d", &gen.TargetWidth, &gen.TargetHeight); err != nil {
			return err
		}
	}

	args := flags.Args()
	if len(args) != 1 {
		return fmt.Errorf("missing <input> arg")
	}

	input := args[0]
	img, err := decode(input)
	if err != nil {
		return err
	}

	if mapFile != "" {
		mapBts, err := os.ReadFile(mapFile)
		if err != nil {
			return err
		}
		var imap = ImageMap{Gen: &gen}
		var dec = json.NewDecoder(bytes.NewReader(mapBts))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&imap); err != nil {
			return err
		}

		for idx, area := range imap.Areas {
			sub := subImage(img, area.Rect())
			out, err := area.Gen.Build(sub)
			if err != nil {
				return err
			}

			if idx > 0 {
				fmt.Println()
			}
			fmt.Println(out)
		}

	} else {
		out, err := gen.Build(img)
		if err != nil {
			return err
		}

		fmt.Println(out)
	}

	return nil
}

type Generator struct {
	PaletteChars  string `json:"paletteChars,omitempty"`
	Invert        bool   `json:"invert,omitempty"`
	TargetWidth   int    `json:"targetWidth,omitempty"`
	TargetHeight  int    `json:"targetHeight,omitempty"`
	Scaler        string `json:"scaler,omitempty"`
	Renderer      string `json:"renderer,omitempty"`
	VarName       string `json:"varName,omitempty"`
	PaletteOffset int    `json:"paletteOffset,omitempty"`
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
	palimg, err := quant.ToPaletted(len(g.PaletteChars), img, nil)
	if err != nil {
		return "", err
	}

	// Sort colors by intensity (HSP colour space):
	colors := uniqueColors(palimg)
	sort.Slice(colors, func(i, j int) bool {
		if g.Invert {
			return hsp(palimg.Palette[colors[i]]) > hsp(palimg.Palette[colors[j]])
		} else {
			return hsp(palimg.Palette[colors[i]]) < hsp(palimg.Palette[colors[j]])
		}
	})

	// Map the unique, sorted colors back to the palette characters:
	colorMap := [256]byte{}
	for i, v := range colors {
		colorMap[v] = g.PaletteChars[i]
	}

	var b bytes.Buffer
	png.Encode(&b, palimg)
	os.WriteFile("/tmp/s.png", b.Bytes(), 0600)

	var out bytes.Buffer
	var renderCtx = &renderContext{
		colors,
		colorMap,
		g,
		palimg,
	}
	if err := render(g.Renderer, renderCtx, &out); err != nil {
		return "", err
	}

	return out.String(), nil
}

type Area struct {
	X   int        `json:"x"`
	Y   int        `json:"y"`
	W   int        `json:"w"`
	H   int        `json:"h"`
	Gen *Generator `json:"gen,omitempty"`
}

func (a Area) Rect() image.Rectangle {
	return image.Rect(a.X, a.Y, a.X+a.W, a.Y+a.H)
}

type ImageMap struct {
	Areas []Area     `json:"areas"`
	Gen   *Generator `json:"gen,omitempty"`
}

func (im *ImageMap) UnmarshalJSON(b []byte) error {
	var tmp struct {
		Gen   *Generator
		Areas []json.RawMessage
	}
	im.Gen = im.Gen.Clone()
	tmp.Gen = im.Gen
	var dec = json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tmp); err != nil {
		return err
	}
	im.Areas = make([]Area, len(tmp.Areas))
	for idx, a := range tmp.Areas {
		im.Areas[idx].Gen = im.Gen.Clone()
		var areaDec = json.NewDecoder(bytes.NewReader(a))
		areaDec.DisallowUnknownFields()
		if err := areaDec.Decode(&im.Areas[idx]); err != nil {
			return fmt.Errorf("invalid area %d: %w", idx, err)
		}
	}
	return nil
}

type renderContext struct {
	colors   []uint8
	colorMap [256]byte
	gen      *Generator
	img      *image.Paletted
}

func render(renderer string, renderCtx *renderContext, buf *bytes.Buffer) error {
	switch renderer {
	case "cpp17":
		return renderCPP17(renderCtx, buf)
	case "cpp":
		return renderCPP(renderCtx, buf)
	case "js":
		return renderJS(renderCtx, buf)
	default:
		return fmt.Errorf("unknown renderer")
	}
}

func renderJS(renderCtx *renderContext, out *bytes.Buffer) error {
	gen := renderCtx.gen

	// Sad that it has come to this:
	out.WriteString("// prettier-ignore\n")

	out.WriteString(fmt.Sprintf("const %s = (() => {\n", gen.VarName))

	seenChars := mapSeenChars(renderCtx.img, renderCtx.colorMap)
	out.WriteString("  const ")
	pIdx := 0
	for idx := range renderCtx.colors {
		char := renderCtx.gen.PaletteChars[idx]
		if seenChars[char] {
			if pIdx > 0 {
				out.WriteString(", ")
			}
			pIdx++
			out.WriteString(fmt.Sprintf("%c=%d", char, idx+gen.PaletteOffset))
		}
	}
	out.WriteString(";\n")

	out.WriteString("  return [\n")
	width := renderCtx.img.Bounds().Dx()
	for y := 0; y < renderCtx.img.Bounds().Dy(); y++ {
		out.WriteString("    ")
		for x := 0; x < width; x++ {
			px := renderCtx.img.ColorIndexAt(x, y)
			char := renderCtx.colorMap[px]
			out.WriteByte(char)
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("  ];\n")
	out.WriteString("})();\n")

	return nil
}

func renderCPP(renderCtx *renderContext, out *bytes.Buffer) error {
	gen := renderCtx.gen

	for idx := range renderCtx.colors {
		out.WriteString(fmt.Sprintf("#define %c %d\n",
			gen.PaletteChars[idx], idx+gen.PaletteOffset))
	}
	out.WriteByte('\n')

	out.WriteString("static const std::array<uint8_t, ")
	out.WriteString(fmt.Sprintf("%d*%d", renderCtx.img.Bounds().Dx(), renderCtx.img.Bounds().Dy()))
	out.WriteString(fmt.Sprintf("> %s = {{\n", gen.VarName))

	width := renderCtx.img.Bounds().Dx()
	for y := 0; y < renderCtx.img.Bounds().Dy(); y++ {
		out.WriteString("    ")
		for x := 0; x < width; x++ {
			px := renderCtx.img.ColorIndexAt(x, y)
			char := renderCtx.colorMap[px]
			out.WriteByte(char)
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("}};\n")
	out.WriteByte('\n')

	for idx := range renderCtx.colors {
		out.WriteString(fmt.Sprintf("#undef %c\n", gen.PaletteChars[idx]))
	}
	out.WriteByte('\n')

	return nil
}

func renderCPP17(renderCtx *renderContext, out *bytes.Buffer) error {
	gen := renderCtx.gen

	seenChars := mapSeenChars(renderCtx.img, renderCtx.colorMap)

	szStr := fmt.Sprintf("%d*%d", renderCtx.img.Bounds().Dx(), renderCtx.img.Bounds().Dy())
	out.WriteString(fmt.Sprintf("static const auto %s = []() constexpr -> const std::array<uint8_t, %s> {\n", gen.VarName, szStr))
	out.WriteString("    const uint8_t ")
	pIdx := 0
	for idx := range renderCtx.colors {
		char := renderCtx.gen.PaletteChars[idx]
		if seenChars[char] {
			if pIdx > 0 {
				out.WriteString(", ")
			}
			pIdx++
			out.WriteString(fmt.Sprintf("%c=%d", char, idx+gen.PaletteOffset))
		}
	}
	out.WriteString(";\n")

	out.WriteString("    return {{\n")
	sz := renderCtx.img.Bounds().Size()
	for y := 0; y < sz.Y; y++ {
		out.WriteString("        ")
		for x := 0; x < sz.X; x++ {
			px := renderCtx.img.ColorIndexAt(x, y)
			char := renderCtx.colorMap[px]
			out.WriteByte(char)
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("    }};\n")
	out.WriteString("}();\n\n")

	return nil
}

func findScaler(v string) draw.Scaler {
	switch v {
	case "nn":
		return draw.NearestNeighbor
	case "approxbilinear":
		return draw.ApproxBiLinear
	case "bilinear":
		return draw.BiLinear
	case "catmullrom", "":
		return draw.CatmullRom
	default:
		return nil
	}
}

func decode(input string) (image.Image, error) {
	bts, err := os.ReadFile(input)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(input)
	switch ext {
	case ".png":
		return png.Decode(bytes.NewReader(bts))
	case ".bmp":
		return bmp.Decode(bytes.NewReader(bts))
	case ".tiff":
		return tiff.Decode(bytes.NewReader(bts))
	case ".gif":
		return gif.Decode(bytes.NewReader(bts))
	case ".webp":
		return webp.Decode(bytes.NewReader(bts))
	case ".jpg", ".jpeg":
		return jpeg.Decode(bytes.NewReader(bts))
	default:
		return nil, fmt.Errorf("unsupported image format")
	}
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

func mapSeenChars(img *image.Paletted, colorMap [256]byte) map[byte]bool {
	out := map[byte]bool{}
	width := img.Bounds().Dx()
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < width; x++ {
			px := img.ColorIndexAt(x, y)
			char := colorMap[px]
			out[char] = true
		}
	}
	return out
}

func uniqueColors(palimg *image.Paletted) []uint8 {
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
