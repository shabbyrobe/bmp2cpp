package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
)

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
