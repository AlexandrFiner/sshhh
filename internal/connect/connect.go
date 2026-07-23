// Package connect builds an ssh command line from a resolved host and hands
// control to the system ssh binary. We intentionally shell out to ssh so we
// inherit ~/.ssh/config, the agent, known_hosts and ProxyJump for free.
package connect

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/alexandrfiner/sshhh/internal/config"
)

// Args returns the ssh argument vector (argv[0] == "ssh") for host h.
// If root is true, an interactive TTY is requested and `sudo -i` is run.
func Args(h config.Host, root bool) []string {
	sudo := h.Sudo || root

	argv := []string{"ssh"}
	if h.Identity != "" {
		argv = append(argv, "-i", h.Identity)
	}
	if h.Jump != "" {
		argv = append(argv, "-J", h.Jump)
	}
	if h.Port != 0 && h.Port != 22 {
		argv = append(argv, "-p", strconv.Itoa(h.Port))
	}
	if sudo {
		argv = append(argv, "-t")
	}

	target := h.Addr
	if h.User != "" {
		target = h.User + "@" + h.Addr
	}
	argv = append(argv, target)

	if sudo {
		argv = append(argv, "sudo -i")
	}
	return argv
}

// Command renders argv as a copy-pasteable shell command (for --dry-run).
func Command(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t") {
			parts[i] = strconv.Quote(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// Run replaces the current process with ssh so the user gets a clean session
// with no lingering parent (signals, TTY, exit code all belong to ssh).
func Run(argv []string) error {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}
	return syscall.Exec(path, argv, os.Environ())
}
