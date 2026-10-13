//go:build !windows

package mygo

import "os"

func fileWatchOpenRoot(path string) (*os.Root, error) { return os.OpenRoot(path) }
