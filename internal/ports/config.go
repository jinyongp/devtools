package ports

import (
	"bytes"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/pelletier/go-toml/v2"
)

func DefaultRange(path string) ([]int, *protocol.Error) {
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return []int{10000, 19999}, nil
	}
	if e != nil {
		return nil, storageError()
	}
	var c struct {
		Port struct {
			Range []int `toml:"range"`
		} `toml:"port"`
	}
	d := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields()
	if d.Decode(&c) != nil || c.Port.Range != nil && !project.ValidRange(c.Port.Range) {
		return nil, fail("invalid_config")
	}
	if c.Port.Range == nil {
		return []int{10000, 19999}, nil
	}
	return c.Port.Range, nil
}
