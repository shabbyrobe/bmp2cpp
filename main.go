package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"

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
	const defaultPaletteChars = "_cowgCONW"

	var sizeRaw string
	var mapFile string
	var gen Generator
	var err error

	if err := gen.Palette.Set(defaultPaletteChars); err != nil {
		panic(err)
	}

	flags := flag.NewFlagSet("", 0)
	flags.StringVar(&sizeRaw, "size", "", "Size, in '<w>x<h>' format. <=0 for either dimension for aspect.")
	flags.Var(&gen.Palette, "chars", fmt.Sprintf("Palette, ordered from least to most intense (HSP colorspace). May be a string of chars, where palette index is determined by rune index, i.e. 'oxXW', or a comma separated list of char/index pairs, i.e. 'o=0,x=1,X=2,W=3'. Chars must be valid in a C++ identifier. Default: %s", defaultPaletteChars))
	flags.StringVar(&gen.Scaler, "scaler", "catmullrom", "Scaler when resizing. Values: nn, approxbilinear, bilinear, catmullrom.")
	flags.StringVar(&gen.Renderer, "renderer", "cpp17", "Renderer. Values: cpp17, cpp, cjs, js.")
	flags.StringVar(&gen.VarName, "var", "bitmap", "Output variable name.")
	flags.BoolVar(&gen.RowWiseJS, "jsrow", true, "When rendering for javascript, output each row as a Uint8Array, rather than the whole image.")
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
