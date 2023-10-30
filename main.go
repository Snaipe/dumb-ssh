package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"time"

	"github.com/alecthomas/kong"
	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	"golang.org/x/text/encoding/unicode"
)

var windowsUTF16 = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)

var cli struct {
	Chdir     string
	Env       []string
	Bind      string        `default:":2222"`
	Shell     string        `default:"sh"`
	WaitDelay time.Duration `default:"100ms"`
}

func main() {
	kong.Parse(&cli)

	var forwardHandler ssh.ForwardedTCPHandler

	server := ssh.Server{
		Addr: cli.Bind,
		Handler: handleShell,
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": handleSftp,
		},

		// Allow port-forwarding
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			return true
		}),
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			return true
		}),
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
	}

	server.SetOption(ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		return true // allow all keys
	}))
	server.SetOption(ssh.PasswordAuth(func(ctx ssh.Context, passwd string) bool {
		return true // allow all users+passwords
	}))

	slog.Info("starting ssh server", "address", cli.Bind)

	err := server.ListenAndServe()
	slog.Info("stopping ssh server", "error", err)
}

func handleShell(s ssh.Session) {
	logger := slog.With("client", s.RemoteAddr(), "subsystem", "shell")

	rawcmd := s.RawCommand()

	var args []string
	if rawcmd != "" {
		switch cli.Shell {
		case "sh", "bash", "zsh", "ash", "dash", "fish":
			args = append(args, "-c", rawcmd)
		case "powershell", "pwsh":
			utfenc, err := windowsUTF16.NewEncoder().String(rawcmd)
			if err != nil {
				logger.Error("encoding command to utf16le", "command", rawcmd, "error", err)
				fmt.Fprintf(s, "failed to encode command to utf16le: %v\n", err)
				s.Exit(127)
				return
			}
			b64enc := base64.StdEncoding.EncodeToString([]byte(utfenc))
			args = append(args, "-EncodedCommand", b64enc)
		}
	}

	cmd := exec.CommandContext(s.Context(), cli.Shell, args...)
	cmd.Env = append(s.Environ(), cli.Env...)
	cmd.Dir = cli.Chdir

	logger.Info("start", "command", cmd.Args)

	signals := make(chan ssh.Signal, 1)
	s.Signals(signals)
	defer s.Signals(nil)

	ptyReq, _, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		pty, err := StartPTY(cmd)
		if err != nil {
			fmt.Fprintf(s, "cannot start program: %v\n", err)
			s.Exit(127)
			return
		}
		go io.Copy(pty, s)
		go io.Copy(s, pty)
	} else {
		// We need to use StdinPipe, otherwise Wait may end up blocking
		// until a full line of text has been sent by the client, even
		// if the program already exited.
		stdin, err := cmd.StdinPipe()
		if err != nil {
			fmt.Fprintf(s, "failed to set up program stdin: %v\n", err)
			s.Exit(127)
			return
		}
		cmd.Stdout = s
		cmd.Stderr = s
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(s, "cannot start program: %v\n", err)
			s.Exit(127)
			return
		}
		go io.Copy(stdin, s)
	}
	logger.Info("waiting")

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait(); logger.Info("wait done") }()

Wait:
	for {
		select {
		case <-waitErr:
			break Wait
		case sig := <-signals:
			cmd.Process.Signal(SignalNum(sig))
		}
	}

	logger.Error("exit", "status", cmd.ProcessState.String())
	s.Exit(cmd.ProcessState.ExitCode())
}

func handleSftp(s ssh.Session) {
	logger := slog.With("client", s.RemoteAddr(), "subsystem", "sftp")

	var opts []sftp.ServerOption
	if cli.Chdir != "" {
		opts = append(opts, sftp.WithServerWorkingDirectory(cli.Chdir))
	}

	server, err := sftp.NewServer(s, opts...)
	if err != nil {
		logger.Info("sftp server init", "error", err)
		return
	}
	err = server.Serve()
	switch {
	case err == io.EOF:
		server.Close()
		logger.Info("client disconnected")
	case err != nil:
		logger.Error("serve", "error", err)
	}
}
