package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const serviceName = "dporter"
const serviceFile = "/etc/systemd/system/dporter.service"

var unitTemplate = `[Unit]
Description=dporter - Docker Container Manager
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
ExecStart=%s %s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

func buildServiceArgs(args []string) string {
	var filtered []string
	skipNext := false
	for i, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "-service" || a == "--service" {
			if i+1 < len(args) {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(a, "-service=") || strings.HasPrefix(a, "--service=") {
			continue
		}
		filtered = append(filtered, a)
	}
	return strings.Join(filtered, " ")
}

func serviceInstall() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	svcArgs := buildServiceArgs(os.Args[1:])
	content := fmt.Sprintf(unitTemplate, bin, svcArgs)

	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	fmt.Printf("Installed %s\n", serviceFile)

	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	if err := run("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	fmt.Println("systemctl daemon-reload OK")

	if err := run("systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	fmt.Printf("Enabled %s.service\n", serviceName)

	if err := run("systemctl", "start", serviceName); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Printf("Started %s.service\n", serviceName)
	return nil
}

func serviceUninstall() error {
	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	_ = run("systemctl", "stop", serviceName)
	_ = run("systemctl", "disable", serviceName)

	if err := os.Remove(serviceFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	fmt.Printf("Removed %s\n", serviceFile)

	_ = run("systemctl", "daemon-reload")
	fmt.Println("Uninstalled dporter service")
	return nil
}

func serviceStatus() error {
	cmd := exec.Command("systemctl", "status", serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
