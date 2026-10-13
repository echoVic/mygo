package mygo

import (
	"os"
	"path/filepath"
)

// OpenRoot uses an ordinary Windows handle that prevents deletion and rename.
// Root.OpenRoot shares deletion, so retain that anchored handle instead and
// release the temporary parent before returning. A drive's root has no
// parent, and can't be renamed anyway.
func fileWatchOpenRoot(path string) (*os.Root, error) {
	if filepath.Dir(path) == path {
		return os.OpenRoot(path)
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return parent.OpenRoot(filepath.Base(path))
}
