//go:build windows
// +build windows

package hwinfostreamdeckplugin

import (
	"errors"
	"os"

	"github.com/shayne/go-winpeg"
	"golang.org/x/sys/windows"
)

type winJobGroup struct{ h winpeg.ProcessExitGroup }

func (g *winJobGroup) dispose() error { return g.h.Dispose() }

func newProcessExitGroup() (processExitGroup, error) {
	h, err := winpeg.NewProcessExitGroup()
	if err != nil {
		return nil, err
	}
	return &winJobGroup{h}, nil
}

func attachProcessToJob(g processExitGroup, p *os.Process) error {
	wg, ok := g.(*winJobGroup)
	if !ok {
		return errors.New("not a win job group")
	}
	if p == nil || p.Pid == 0 {
		return errors.New("process not started")
	}

	// Try the existing winpeg path first.
	if err := wg.h.AddProcess(p); err == nil {
		return nil
	}

	// Fallback: open handle by PID (avoids unsafe handle layout issues).
	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(p.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	return windows.AssignProcessToJobObject(windows.Handle(wg.h), handle)
}
