package main

import (
	"bytes"
	"fmt"
	"image"
)

type renderContext struct {
	paletteIndexes     []uint8
	paletteIndexToChar [256]rune
	gen                *Generator
	img                *image.Paletted
}

func render(gen *Generator, renderCtx *renderContext, buf *bytes.Buffer) error {
	switch gen.Renderer {
	case "cpp17":
		return renderCPP17(renderCtx, buf)
	case "cpp":
		return renderCPP(renderCtx, buf)
	case "cjs":
		return renderJS(renderCtx, buf, false, gen.RowWiseJS)
	case "js":
		return renderJS(renderCtx, buf, true, gen.RowWiseJS)
	default:
		return fmt.Errorf("unknown renderer")
	}
}

func renderJS(renderCtx *renderContext, out *bytes.Buffer, esm bool, rowWiseJS bool) error {
	gen := renderCtx.gen
	pal := gen.Palette

	// Sad that it has come to this:
	out.WriteString("// prettier-ignore deno-fmt-ignore\n")

	if esm {
		out.WriteString(fmt.Sprintf("export const %s = (() => {\n", gen.VarName))
	} else {
		out.WriteString(fmt.Sprintf("exports.%s = (() => {\n", gen.VarName))
	}

	seenChars := mapSeenChars(renderCtx.img, renderCtx.paletteIndexToChar)
	out.WriteString("  const ")
	pIdx := 0
	for intensity := range renderCtx.paletteIndexes {
		char := pal.IntensityRune[intensity]
		if seenChars[char] {
			if pIdx > 0 {
				out.WriteString(", ")
			}
			pIdx++
			out.WriteString(fmt.Sprintf("%c=%d", char,
				pal.IntensityIndex[intensity]+uint8(gen.PaletteOffset)))
		}
	}
	out.WriteString(";\n")

	if !rowWiseJS {
		out.WriteString("  return new Uint8Array([\n")
	} else {
		out.WriteString("  return Object.freeze([\n")
	}

	width := renderCtx.img.Bounds().Dx()
	for y := 0; y < renderCtx.img.Bounds().Dy(); y++ {
		out.WriteString("    ")
		if rowWiseJS {
			out.WriteString("  new Uint8Array([")
		}
		for x := 0; x < width; x++ {
			px := renderCtx.img.ColorIndexAt(x, y)
			char := renderCtx.paletteIndexToChar[px]
			out.WriteRune(char)
			out.WriteByte(',')
		}
		if rowWiseJS {
			out.WriteString("]),")
		}
		out.WriteByte('\n')
	}

	out.WriteString("  ]);\n")
	out.WriteString("})();\n")

	return nil
}

func renderCPP(renderCtx *renderContext, out *bytes.Buffer) error {
	gen := renderCtx.gen
	pal := gen.Palette

	for intensity := range renderCtx.paletteIndexes {
		out.WriteString(fmt.Sprintf("#define %c %d\n",
			pal.IntensityRune[intensity],
			pal.IntensityIndex[intensity]+uint8(gen.PaletteOffset)))
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
			char := renderCtx.paletteIndexToChar[px]
			out.WriteRune(char)
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("}};\n")
	out.WriteByte('\n')

	for intensity := range renderCtx.paletteIndexes {
		out.WriteString(fmt.Sprintf("#undef %c\n",
			pal.IntensityRune[intensity]))
	}
	out.WriteByte('\n')

	return nil
}

func renderCPP17(renderCtx *renderContext, out *bytes.Buffer) error {
	gen := renderCtx.gen
	pal := renderCtx.gen.Palette

	seenChars := mapSeenChars(renderCtx.img, renderCtx.paletteIndexToChar)

	szStr := fmt.Sprintf("%d*%d", renderCtx.img.Bounds().Dx(), renderCtx.img.Bounds().Dy())
	out.WriteString(fmt.Sprintf("static const auto %s = []() constexpr -> const std::array<uint8_t, %s> {\n", gen.VarName, szStr))
	out.WriteString("    const uint8_t ")
	pIdx := 0
	for intensity := range renderCtx.paletteIndexes {
		char := pal.IntensityRune[intensity]
		if seenChars[char] {
			if pIdx > 0 {
				out.WriteString(", ")
			}
			pIdx++
			out.WriteString(fmt.Sprintf("%c=%d", char,
				pal.IntensityIndex[intensity]+uint8(gen.PaletteOffset)))
		}
	}
	out.WriteString(";\n")

	out.WriteString("    return {{\n")
	sz := renderCtx.img.Bounds().Size()
	for y := 0; y < sz.Y; y++ {
		out.WriteString("        ")
		for x := 0; x < sz.X; x++ {
			px := renderCtx.img.ColorIndexAt(x, y)
			char := renderCtx.paletteIndexToChar[px]
			out.WriteRune(char)
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("    }};\n")
	out.WriteString("}();\n\n")

	return nil
}
