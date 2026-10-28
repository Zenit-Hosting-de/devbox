package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/api"
)

// socketName is the file name of the Unix socket below $XDG_RUNTIME_DIR/devbox.
const socketName = "api.sock"

type serveFlags struct {
	listen       string
	open         bool
	socket       string
	allowOrigins []string
}

func (a *app) serveCommand() *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the REST API and the web interface",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return a.serve(ctx, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.listen, "listen", "", "address to listen on (default: server.listen)")
	fl.BoolVar(&f.open, "open", false, "open the web interface in the browser")
	fl.StringVar(&f.socket, "socket", "", "Unix socket (default: $XDG_RUNTIME_DIR/devbox/api.sock)")
	fl.StringArrayVar(&f.allowOrigins, "allow-origin", nil, "additionally allow this origin for this run (repeatable)")
	return cmd
}

// isLoopback reports whether a listen address is reachable from this machine only.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func (a *app) serve(ctx context.Context, f serveFlags) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	sc, err := st.Server()
	if err != nil {
		return err
	}
	listen := f.listen
	if listen == "" {
		listen = sc.Listen
	}
	logger := slog.New(slog.NewJSONHandler(a.io.Stderr, nil))

	switch sc.Auth {
	case "pam":
		return &exitError{code: ExitInvalid, err: errors.New("server.auth pam needs the login and TLS, which are not available yet; use server.auth local on a loopback address")}
	case "off":
		logger.Warn("server.auth is off: there is no login; use this for tests only")
	}
	if !isLoopback(listen) {
		return &exitError{code: ExitInvalid, err: fmt.Errorf("%s is not a loopback address: it needs server.auth pam and TLS, not available yet", listen)}
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	lns := []net.Listener{ln}
	defer func() { _ = ln.Close() }()

	srv, err := api.New(api.Options{
		Build: a.build, Env: a.coreEnv(), Listen: ln.Addr().String(),
		Origins: append(append([]string{}, sc.AllowedOrigins...), f.allowOrigins...),
		Auth:    sc.Auth, Logger: logger,
	})
	if err != nil {
		return err
	}

	socketPath := f.socket
	if socketPath == "" {
		if rt := st.Ctx.XDG.Runtime; rt != "" {
			socketPath = filepath.Join(rt, "devbox", socketName)
		} else {
			logger.Warn("no Unix socket: XDG_RUNTIME_DIR is not set and --socket is not given")
		}
	}
	if socketPath != "" {
		sl, err := listenSocket(ctx, socketPath)
		if err != nil {
			return err
		}
		defer func() { _ = sl.Close() }()
		lns = append(lns, sl)
		srv.SetSocket(true)
		logger.Info("listening", "socket", socketPath)
	}

	url := "http://" + ln.Addr().String()
	logger.Info("listening", "url", url)
	if f.open {
		if err := a.openBrowser(ctx, url); err != nil {
			logger.Warn("cannot open the browser", "error", err)
		}
	}
	if err := srv.Serve(ctx, lns...); err != nil {
		return err
	}
	logger.Info("stopped")
	return nil
}

// listenSocket creates the socket (0600) and removes a stale socket of a process that
// is gone. A missing socket directory is created with mode 0700; an existing directory
// (for example $HOME or /tmp given with --socket) belongs to the user and keeps its
// mode, the socket file itself is what restricts access.
func listenSocket(ctx context.Context, path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	_, statErr := os.Stat(dir)
	created := errors.Is(statErr, fs.ErrNotExist)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if created {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a private directory needs the execute bit
			return nil, fmt.Errorf("socket: %w", err)
		}
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket: %s exists and is not a socket", path)
		}
		dialer := net.Dialer{Timeout: time.Second}
		if c, err := dialer.DialContext(ctx, "unix", path); err == nil {
			_ = c.Close()
			return nil, fmt.Errorf("socket: %s is in use; is devbox serve already running?", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("socket: %w", err)
		}
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("socket: %w", err)
	}
	return ln, nil
}

// openBrowser starts $BROWSER, else xdg-open, without waiting for it.
func (a *app) openBrowser(ctx context.Context, url string) error {
	opener := a.io.Getenv("BROWSER")
	if opener == "" {
		opener = "xdg-open"
	}
	fields := strings.Fields(opener)
	cmd := exec.CommandContext(context.WithoutCancel(ctx), fields[0], append(fields[1:], url)...) //nolint:gosec // the user's own browser
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
