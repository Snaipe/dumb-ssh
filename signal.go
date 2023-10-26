package main

import (
	"os"
	"syscall"

	"github.com/gliderlabs/ssh"
)

// NOTE: these are all of the POSIX signals supported by the ssh spec.
// We do not care about supporting other signals.
//
// We can't directly use golang.org/x/sys/unix.SignalNum on unices because the
// name map uses the full signal name (i.e. SIGABRT instead of ABRT).

var signals = map[ssh.Signal]syscall.Signal{
	ssh.SIGABRT: syscall.SIGABRT,
	ssh.SIGALRM: syscall.SIGALRM,
	ssh.SIGFPE:  syscall.SIGFPE,
	ssh.SIGHUP:  syscall.SIGHUP,
	ssh.SIGILL:  syscall.SIGILL,
	ssh.SIGINT:  syscall.SIGINT,
	ssh.SIGKILL: syscall.SIGKILL,
	ssh.SIGPIPE: syscall.SIGPIPE,
	ssh.SIGQUIT: syscall.SIGQUIT,
	ssh.SIGSEGV: syscall.SIGSEGV,
	ssh.SIGTERM: syscall.SIGTERM,
}

func SignalNum(signame ssh.Signal) os.Signal {
	sig, ok := signals[signame]
	if !ok {
		sig = syscall.SIGKILL
	}
	return sig
}
