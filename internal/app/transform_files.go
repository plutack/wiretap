package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/plutack/wiretap/internal/scripting"
)

const (
	TransformFileFormat  = "wiretap-transform"
	TransformFileVersion = 1
	maxTransformFileSize = 1024 * 1024
)

// TransformDefinition is the portable, database-independent representation of
// a Wiretap transform. IDs and timestamps are deliberately excluded so an
// imported file always creates a new, reviewable draft.
type TransformDefinition struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Name     string `json:"name"`
	Trigger  string `json:"trigger"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	Program  string `json:"program"`
}

// EncodeTransformFile validates and formats a portable transform file.
func EncodeTransformFile(def TransformDefinition) ([]byte, error) {
	def.Format = TransformFileFormat
	def.Version = TransformFileVersion
	if err := validateTransformDefinition(def); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("app: encode transform file: %w", err)
	}
	if len(out)+1 > maxTransformFileSize {
		return nil, fmt.Errorf("app: transform file exceeds %d bytes", maxTransformFileSize)
	}
	return append(out, '\n'), nil
}

// DecodeTransformFile parses one strict, bounded transform file. Unknown
// fields are rejected so misspelled configuration cannot be silently lost.
func DecodeTransformFile(data []byte) (TransformDefinition, error) {
	if len(data) > maxTransformFileSize {
		return TransformDefinition{}, fmt.Errorf("app: transform file exceeds %d bytes", maxTransformFileSize)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var def TransformDefinition
	if err := dec.Decode(&def); err != nil {
		return TransformDefinition{}, fmt.Errorf("app: decode transform file: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return TransformDefinition{}, err
	}
	if err := validateTransformDefinition(def); err != nil {
		return TransformDefinition{}, err
	}
	return def, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("app: decode transform file: %w", err)
	}
	return errors.New("app: transform file contains more than one JSON value")
}

func validateTransformDefinition(def TransformDefinition) error {
	if def.Format != TransformFileFormat {
		return fmt.Errorf("app: unsupported transform format %q", def.Format)
	}
	if def.Version != TransformFileVersion {
		return fmt.Errorf("app: unsupported transform file version %d", def.Version)
	}
	if strings.TrimSpace(def.Name) == "" {
		return errors.New("app: transform name is required")
	}
	if !scripting.Trigger(def.Trigger).Valid() {
		return fmt.Errorf("app: invalid trigger %q", def.Trigger)
	}
	return nil
}
