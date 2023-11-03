package main

import (
	"io"
)

type PTY interface {
	io.ReadWriteCloser
	Resize(width, height int) error
}

type WaitFunc func() (int, error)
