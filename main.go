package main

import (
	"bytes"
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
	var paletteChars string
	var invert bool
	var sizeRaw string
	var targetWidth int
	var targetHeight int
	var scaler string
	var renderer string
	var varName string

	flags := flag.NewFlagSet("", 0)
	flags.StringVar(&paletteChars, "pal", "_cowgCONW", "Palette chars, ordered from least to most intense. Must be valid C++ identifier characters.")
	flags.StringVar(&sizeRaw, "size", "", "Size, in '<w>x<h>' format. <=0 for either dimension for aspect.")
	flags.StringVar(&scaler, "scaler", "catmullrom", "Scaler when resizing. Values: nn, approxbilinear, bilinear, catmullrom.")
	flags.StringVar(&renderer, "renderer", "cpp", "Renderer. Values: cpp, js.")
	flags.StringVar(&varName, "var", "bitmap", "Output variable name.")
	flags.BoolVar(&invert, "invert", false, "Invert colours")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	if len(sizeRaw) > 0 {
		if _, err := fmt.Sscanf(sizeRaw, "%dx%d", &targetWidth, &targetHeight); err != nil {
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

	// Rescale:
	if targetWidth > 0 || targetHeight > 0 {
		newSize := prepareSize(targetWidth, targetHeight, img.Bounds().Size())
		nb := image.Rectangle{Max: newSize}
		dst := image.NewRGBA(nb)
		scl := findScaler(scaler)
		scl.Scale(dst, nb, img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	// Quantise:
	quant := wu2quant.NewQuantizer()
	palimg, err := quant.ToPaletted(len(paletteChars), img)
	if err != nil {
		return err
	}

	// Sort colors by intensity (HSP colour space):
	colors := uniqueColors(palimg)
	sort.Slice(colors, func(i, j int) bool {
		if invert {
			return hsp(palimg.Palette[colors[i]]) > hsp(palimg.Palette[colors[j]])
		} else {
			return hsp(palimg.Palette[colors[i]]) < hsp(palimg.Palette[colors[j]])
		}
	})

	// Map the unique, sorted colors back to the palette characters:
	colorMap := [256]byte{}
	for i, v := range colors {
		colorMap[v] = paletteChars[i]
	}

	var out bytes.Buffer
	var renderCtx = &renderContext{
		colors,
		colorMap,
		paletteChars,
		palimg,
		varName,
	}
	if err := render(renderer, renderCtx, &out); err != nil {
		return err
	}
	fmt.Println(out.String())

	return nil
}

type renderContext struct {
	colors       []uint8
	colorMap     [256]byte
	paletteChars string
	img          *image.Paletted
	varName      string
}

func render(renderer string, renderCtx *renderContext, buf *bytes.Buffer) error {
	switch renderer {
	case "cpp":
		return renderCPP(renderCtx, buf)
	case "js":
		return renderJS(renderCtx, buf)
	default:
		return fmt.Errorf("unknown renderer")
	}
}

func renderJS(renderCtx *renderContext, out *bytes.Buffer) error {
	// Sad that it has come to this:
	out.WriteString("// prettier-ignore\n")

	out.WriteString(fmt.Sprintf("const %s = (() => {\n", renderCtx.varName))
	for idx := range renderCtx.colors {
		out.WriteString(fmt.Sprintf("  const %c = %d;\n", renderCtx.paletteChars[idx], idx))
	}
	out.WriteByte('\n')

	out.WriteString("  return [\n")

	yoff := 0
	width := renderCtx.img.Bounds().Dx()
	for y := 0; y < renderCtx.img.Bounds().Dy(); y++ {
		out.WriteString("    ")
		for x := 0; x < width; x++ {
			px := renderCtx.img.Pix[yoff+x]
			char := renderCtx.colorMap[px]
			out.WriteByte(char)
			out.WriteByte(',')
		}
		yoff += width
		out.WriteByte('\n')
	}
	out.WriteString("  ];\n")
	out.WriteString("});\n")

	return nil
}

func renderCPP(renderCtx *renderContext, out *bytes.Buffer) error {
	for idx := range renderCtx.colors {
		out.WriteString(fmt.Sprintf("#define %c %d\n", renderCtx.paletteChars[idx], idx))
	}
	out.WriteByte('\n')

	out.WriteString("static const std::array<uint8_t, ")
	out.WriteString(fmt.Sprintf("%d*%d", renderCtx.img.Bounds().Dx(), renderCtx.img.Bounds().Dy()))
	out.WriteString(fmt.Sprintf("> %s = {{\n", renderCtx.varName))

	yoff := 0
	width := renderCtx.img.Bounds().Dx()
	for y := 0; y < renderCtx.img.Bounds().Dy(); y++ {
		out.WriteString("    ")
		for x := 0; x < width; x++ {
			px := renderCtx.img.Pix[yoff+x]
			char := renderCtx.colorMap[px]
			out.WriteByte(char)
			out.WriteByte(',')
		}
		yoff += width
		out.WriteByte('\n')
	}
	out.WriteString("}};\n")
	out.WriteByte('\n')

	for idx := range renderCtx.colors {
		out.WriteString(fmt.Sprintf("#undef %c\n", renderCtx.paletteChars[idx]))
	}
	out.WriteByte('\n')

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

func uniqueColors(palimg *image.Paletted) []uint8 {
	foundColors := [256]bool{}
	yoff := 0
	width := palimg.Bounds().Dx()
	for y := 0; y < palimg.Bounds().Dy(); y++ {
		for x := 0; x < width; x++ {
			px := palimg.Pix[yoff+x]
			foundColors[px] = true
		}
		yoff += width
	}
	colors := make([]uint8, 0)
	for fc, ok := range foundColors {
		if ok {
			colors = append(colors, uint8(fc))
		}
	}
	return colors
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
