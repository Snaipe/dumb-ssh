//go:build unix
// +build unix

package main

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type ptyimpl struct {
	*os.File
}

func StartPTY(cmd *exec.Cmd) (PTY, WaitFunc, error) {
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, err
	}
	wait := func() (int, error) {
		err := cmd.Wait()
		return cmd.ProcessState.ExitCode(), err
	}
	return &ptyimpl{f}, wait, nil
}

func (p *ptyimpl) Resize(width, height int) error {
	pty.Setsize(p.File, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
	return nil
}
