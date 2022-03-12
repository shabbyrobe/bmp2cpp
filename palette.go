package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Palette struct {
	Size           int
	IntensityRune  [256]rune
	IntensityIndex [256]uint8
}

func (p *Palette) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("could not unmarshal palette: %w", err)
	}
	pp, err := PaletteFromChars(s)
	if err != nil {
		return err
	}
	*p = *pp
	return nil
}

func (p *Palette) String() string {
	var out strings.Builder
	for i := 0; i < p.Size; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteRune(p.IntensityRune[i])
		out.WriteByte('=')
		out.WriteString(strconv.Itoa(int(p.IntensityIndex[i])))
	}
	return out.String()
}

func (p *Palette) Set(v string) error {
	pl, err := PaletteFromChars(v)
	if err != nil {
		return err
	}
	*p = *pl
	return nil
}

var splitPtn = regexp.MustCompile(`,\s*`)

func PaletteFromChars(v string) (*Palette, error) {
	p := &Palette{}

	if strings.Contains(v, "=") {
		bits := splitPtn.Split(v, -1)
		if len(bits) > 256 {
			return nil, fmt.Errorf("too many characters in palette")
		}
		p.Size = len(bits)
		for intensity, bit := range bits {
			pchar, n := utf8.DecodeRuneInString(bit)
			if bit[n] != '=' {
				return nil, fmt.Errorf("expected '=' after rune in palette at intensity %d", intensity)
			}

			idx, err := strconv.ParseUint(bit[n+1:], 10, 8)
			if err != nil {
				return nil, fmt.Errorf("invalid palette index at intensity %d: %w", intensity, err)
			}

			p.IntensityIndex[intensity] = uint8(idx)
			p.IntensityRune[intensity] = pchar
		}

	} else {
		if utf8.RuneCountInString(v) > 256 {
			return nil, fmt.Errorf("too many characters in palette")
		}
		var intensity uint8
		for _, r := range v {
			p.IntensityIndex[intensity] = intensity
			p.IntensityRune[intensity] = r
			intensity++
		}
		p.Size = int(intensity)
	}

	return p, nil
}
