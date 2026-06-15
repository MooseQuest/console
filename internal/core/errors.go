package core

import "errors"

// ErrNotFound is returned by stores and registries when a keyed item does not
// exist. Callers compare with errors.Is.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when creating an item whose key already exists.
var ErrConflict = errors.New("already exists")
