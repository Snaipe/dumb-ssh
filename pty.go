package main

import (
	"io"
)

type PTY interface {
	io.ReadWriteCloser
}
