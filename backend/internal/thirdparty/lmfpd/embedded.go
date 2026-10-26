package lmfpd

// This file is a grok2api addition (not part of upstream lm-detector). It
// loads the reference data embedded at build time so no runtime download is
// needed, and exposes a few read-only helpers.

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sync"
)

//go:embed data/bank.json
var embeddedBank []byte

//go:embed data/shared_detector.bin
var embeddedDetector []byte

var (
	embeddedOnce sync.Once
	embeddedDet  *Detector
	embeddedErr  error
)

// LoadEmbedded returns the process-wide detector built from embedded data.
func LoadEmbedded() (*Detector, error) {
	embeddedOnce.Do(func() {
		b := &bank{}
		if err := json.Unmarshal(embeddedBank, b); err != nil {
			embeddedErr = errors.New("lmfpd: invalid embedded bank: " + err.Error())
			return
		}
		det, err := decodeDetector(embeddedDetector)
		if err != nil {
			embeddedErr = err
			return
		}
		embeddedDet, embeddedErr = newDetector(b, det)
	})
	return embeddedDet, embeddedErr
}

// ModelFamily returns the reference family of a model id ("" when unknown).
func (d *Detector) ModelFamily(model string) string {
	for _, m := range d.bank.Models {
		if m.ID == model {
			return m.Family
		}
	}
	return ""
}

// HasModel reports whether the reference bank contains model.
func (d *Detector) HasModel(model string) bool { return d.ModelFamily(model) != "" }
