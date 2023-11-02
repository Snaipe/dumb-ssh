//go:build windows
// +build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

type fakepty struct {
	io.Reader
	io.Writer
	close func() error
}

func (pty *fakepty) Close() error {
	return pty.close()
}

func StartPTY(cmd *exec.Cmd) (PTY, error) {
	// Not supported on windows, so just make pipes

	iout, iin, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = iin
	cmd.Stderr = iin

	stdin, err := cmd.StdinPipe()
	if err != nil {
		iout.Close()
		iin.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		iout.Close()
		iin.Close()
		stdin.Close()
		return nil, err
	}

	close := func() error {
		return errors.Join(iin.Close(), iout.Close(), stdin.Close())
	}

	return &fakepty{Reader: iout, Writer: stdin, close: close}, nil
}
