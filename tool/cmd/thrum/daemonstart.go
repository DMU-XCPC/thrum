package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// localConfig is this machine's view of one site: where it reaches it and whether
// this machine's daemon announces itself on the LAN. It is never committed.
type localConfig struct {
	Reach    string `yaml:"reach"`
	Discover bool   `yaml:"discover,omitempty"`
	Inactive bool   `yaml:"inactive,omitempty"`
	Code     bool   `yaml:"code,omitempty"`
	Run      bool   `yaml:"run,omitempty"`
}

func readLocalConfig(l layout.Layout, name string) localConfig {
	var c localConfig
	data, err := os.ReadFile(filepath.Join(l.ConfigDir(), name+".yaml"))
	if err != nil {
		return c
	}
	if format.DecodeWhole(data, &c) != nil {
		return localConfig{}
	}
	return c
}

func writeLocalConfig(l layout.Layout, name string, c localConfig) error {
	if err := os.MkdirAll(l.ConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := format.MarshalYAML(c)
	if err != nil {
		return err
	}
	return format.WriteFileAtomic(filepath.Join(l.ConfigDir(), name+".yaml"), data)
}

// ensureDaemon starts this site's daemon in the background when a command needs it.
// The daemon is an implementation detail: a user runs sync, sends a message or says
// hello, and never has to run it. The address is the site's own, or the fixed
// default port on the machine's outbound address.
func ensureDaemon(l layout.Layout) error {
	self := readSiteName(l)
	if self == "" {
		return fmt.Errorf("this machine does not name its site")
	}
	cfg := readLocalConfig(l, self)
	if cfg.Inactive {
		return fmt.Errorf("this site has left the workspace; run `thrum site stay --at <host:port>` to return")
	}
	if _, err := daemon.Status(l); err == nil {
		return nil
	}
	reach := daemon.Reach(l, self)
	if reach == "" {
		reach = daemon.DefaultReach()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--root", l.Root, "daemon", "run", "--listen", reach}
	// The test suite shortens the route interval to see the idle shutdown quickly.
	if v := os.Getenv("THRUM_ROUTE_INTERVAL"); v != "" {
		args = append(args, "--route-interval", v)
	}
	if cfg.Discover {
		args = append(args, "--discover")
	}
	if cfg.Code {
		args = append(args, "--code")
	}
	if cfg.Run {
		args = append(args, "--run")
	}
	logPath := filepath.Join(l.Var(), "daemon.log")
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the daemon: %w", err)
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := daemon.Status(l); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the daemon did not start; see %s", logPath)
}
