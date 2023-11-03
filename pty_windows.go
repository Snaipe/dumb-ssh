//go:build windows
// +build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"math"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

type conpty struct {
	io.Reader
	io.Writer
	pty   windows.Handle
	close func() error
}

func (pty *conpty) Close() error {
	return pty.close()
}

func (pty *conpty) Resize(width, height int) error {
	if width < 0 || width > math.MaxInt16 || height < 0 || height > math.MaxInt16 {
		return strconv.ErrRange
	}
	sz := windows.Coord{X: int16(width), Y: int16(height)}
	return windows.ResizePseudoConsole(pty.pty, sz)
}

func StartPTY(cmd *exec.Cmd) (PTY, WaitFunc, error) {

	// We have to reimplement os.StartProcess ourselves because we simply
	// have no way to pass in the pseudoconsole.
	//
	// TODO: delete everything once os.SysProcAttr has a way to do this
	// on windows.

	var ptyIn, ptsIn windows.Handle
	if err := windows.CreatePipe(&ptsIn, &ptyIn, nil, 0); err != nil {
		return nil, nil, &os.SyscallError{Syscall: "CreatePipe", Err: err}
	}
	defer func() {
		if ptyIn != windows.Handle(0) {
			windows.CloseHandle(ptyIn)
		}
		windows.CloseHandle(ptsIn)
	}()

	var ptyOut, ptsOut windows.Handle
	if err := windows.CreatePipe(&ptyOut, &ptsOut, nil, 0); err != nil {
		return nil, nil, &os.SyscallError{Syscall: "CreatePipe", Err: err}
	}
	defer func() {
		if ptyIn != windows.Handle(0) {
			windows.CloseHandle(ptyIn)
		}
		windows.CloseHandle(ptsIn)
	}()

	winsz := windows.Coord{X: 80, Y: 32}

	var pty windows.Handle
	err := windows.CreatePseudoConsole(winsz, ptsIn, ptsOut, 0, &pty)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "CreatePseudoConsole", Err: err}
	}

	attrlist, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "NewProcThreadAttributeList", Err: err}
	}

	err = attrlist.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(pty),
		unsafe.Sizeof(pty),
	)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "UpdateProcThreadAttributeList", Err: err}
	}

	progname, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "creating utf16 progname", Err: err}
	}

	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "creating utf16 cmdline", Err: err}
	}

	workdir, err := windows.UTF16PtrFromString(cmd.Dir)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "creating utf16 workdir", Err: err}
	}

	var envblock []uint16
	for _, e := range cmd.Env {
		env, err := windows.UTF16FromString(e)
		if err != nil {
			return nil, nil, &os.SyscallError{Syscall: "creating utf16 envp", Err: err}
		}
		envblock = append(envblock, env...)
		envblock = append(envblock, 0)
	}

	var envp *uint16
	if len(envblock) > 0 {
		envp = unsafe.SliceData(envblock)
	}

	var siex windows.StartupInfoEx
	siex.ProcThreadAttributeList = attrlist.List()

	var procinfo windows.ProcessInformation
	err = windows.CreateProcess(
		progname,
		cmdline,
		nil,
		nil,
		true,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		envp,
		workdir,
		&siex.StartupInfo,
		&procinfo,
	)
	if err != nil {
		return nil, nil, &os.SyscallError{Syscall: "CreateProcess", Err: err}
	}

	cmd.Process, err = os.FindProcess(int(procinfo.ProcessId))
	if err != nil {
		windows.CloseHandle(procinfo.Process)
		windows.CloseHandle(procinfo.Thread)
		return nil, nil, err
	}

	wait := func() (int, error) {
		defer windows.CloseHandle(procinfo.Process)
		defer windows.CloseHandle(procinfo.Thread)
		if _, err := windows.WaitForSingleObject(procinfo.Process, windows.INFINITE); err != nil {
			return 127, &os.SyscallError{Syscall: "WaitForSingleObject", Err: err}
		}
		var exit uint32
		if err := windows.GetExitCodeProcess(procinfo.Process, &exit); err != nil {
			return 127, &os.SyscallError{Syscall: "GetExitCodeProcess", Err: err}
		}
		return int(exit), nil
	}

	var (
		in  = os.NewFile(uintptr(ptyIn), "<pty input>")
		out = os.NewFile(uintptr(ptyOut), "<pty output>")
	)

	// Neuter the deferred windows.CloseHandle
	ptyIn = windows.Handle(0)
	ptyOut = windows.Handle(0)

	close := func() error {
		defer windows.ClosePseudoConsole(pty)
		return errors.Join(in.Close(), out.Close())
	}

	return &conpty{Reader: out, Writer: in, pty: pty, close: close}, wait, nil
}
