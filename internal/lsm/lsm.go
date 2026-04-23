package lsm

import "errors"

var ErrNotImplemented = errors.New("lsm: not implemented")

type Tree struct {
	maxLevels int
	algorithm string
}

func New(maxLevels int, algorithm string) *Tree {
	return &Tree{
		maxLevels: maxLevels,
		algorithm: algorithm,
	}
}

func (t *Tree) RegisterTable(level int, path string) error {
	return ErrNotImplemented
}

func (t *Tree) Compact(level int) error {
	return ErrNotImplemented
}
