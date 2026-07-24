//go:build !windows
// +build !windows

package hwinfostreamdeckplugin

import "os"

func newProcessExitGroup() (processExitGroup, error) { return nil, nil }
func attachProcessToJob(_ processExitGroup, _ *os.Process) error { return nil }
