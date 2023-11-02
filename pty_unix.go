//go:build unix
// +build unix

package main

import (
	"os/exec"

	"github.com/creack/pty"
)

func StartPTY(cmd *exec.Cmd) (PTY, error) {
	return pty.Start(cmd)
}
