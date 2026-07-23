// sshhh — a quiet ssh launcher: pick a host by name/group/tag and connect.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alexandrfiner/sshhh/internal/config"
	"github.com/alexandrfiner/sshhh/internal/connect"
	"github.com/alexandrfiner/sshhh/internal/tui"
)

const usage = `sshhh — quickly pick an ssh host and connect

Usage:
  sshhh [flags] [alias|query]   pick a host (or connect directly) and log in
  sshhh [flags] ls              list hosts

Flags:
  -r, --root         request a TTY and run sudo -i on login
  -n, --dry-run      print the ssh command without running it
      --config PATH  path to config (default $SSHHH_CONFIG or
                     ~/.config/sshhh/config.yaml)
  -h, --help         this help
`

type opts struct {
	root       bool
	dryRun     bool
	configPath string
	args       []string
}

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshhh:", err)
		os.Exit(2)
	}

	hosts, err := config.Load(o.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "sshhh: config not found: %s\n", o.configPath)
			fmt.Fprintln(os.Stderr, "create it or point to one via --config / SSHHH_CONFIG")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "sshhh:", err)
		os.Exit(1)
	}

	if len(o.args) > 0 && o.args[0] == "ls" {
		listHosts(hosts)
		return
	}

	var query string
	if len(o.args) > 0 {
		query = o.args[0]
	}

	host, root, dryRun, err := resolve(hosts, query, o.root, o.dryRun, o.configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshhh:", err)
		os.Exit(1)
	}
	if host == nil {
		os.Exit(130) // user quit the browser
	}

	argv := connect.Args(*host, root)
	if dryRun {
		fmt.Println(connect.Command(argv))
		return
	}

	fmt.Fprintf(os.Stderr, "sshhh → %s/%s (%s)\n", host.Group, host.Alias, host.Addr)
	if err := connect.Run(argv); err != nil {
		fmt.Fprintln(os.Stderr, "sshhh:", err)
		os.Exit(1)
	}
}

// resolve picks the host to connect to. A single unambiguous match connects
// directly (fast path); anything else opens the k9s-style browser seeded with
// the query. The browser can toggle root/dry-run, so the possibly-updated
// states are returned. A nil host means the user quit the browser.
func resolve(hosts []config.Host, query string, root, dryRun bool, configPath string) (*config.Host, bool, bool, error) {
	if query == "" {
		return tui.Run(hosts, configPath, "", root, dryRun)
	}

	var exact, fuzzy []config.Host
	for _, h := range hosts {
		switch {
		case h.Alias == query || strings.EqualFold(query, h.Group+"/"+h.Alias):
			exact = append(exact, h) // exact alias or group/alias
		case matchesQuery(h, query):
			fuzzy = append(fuzzy, h)
		}
	}

	// An exact single match wins; otherwise pick the pool to open the browser on.
	switch {
	case len(exact) == 1:
		return &exact[0], root, dryRun, nil
	case len(exact) == 0 && len(fuzzy) == 1:
		return &fuzzy[0], root, dryRun, nil
	case len(exact) == 0 && len(fuzzy) == 0:
		return nil, root, dryRun, fmt.Errorf("no host matched %q", query)
	default:
		return tui.Run(hosts, configPath, query, root, dryRun)
	}
}

func matchesQuery(h config.Host, q string) bool {
	q = strings.ToLower(q)
	hay := strings.ToLower(strings.Join([]string{
		h.Alias, h.Group, h.Name, strings.Join(h.Tags, " "),
	}, " "))
	return strings.Contains(hay, q)
}

func listHosts(hosts []config.Host) {
	if len(hosts) == 0 {
		fmt.Println("(no hosts in config)")
		return
	}
	// hosts are already sorted by group then alias.
	var group string
	width := 0
	for _, h := range hosts {
		if len(h.Alias) > width {
			width = len(h.Alias)
		}
	}
	for _, h := range hosts {
		if h.Group != group {
			group = h.Group
			fmt.Printf("\n%s\n", group)
		}
		meta := h.Name
		if h.Desc != "" {
			if meta != "" {
				meta += " — "
			}
			meta += h.Desc
		}
		fmt.Printf("  %-*s  %s\n", width, h.Alias, meta)
	}
}

func parseArgs(argv []string) (opts, error) {
	o := opts{configPath: config.DefaultPath()}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(usage)
			os.Exit(0)
		case a == "-r" || a == "--root":
			o.root = true
		case a == "-n" || a == "--dry-run":
			o.dryRun = true
		case a == "--config":
			if i+1 >= len(argv) {
				return o, errors.New("--config requires a path")
			}
			i++
			o.configPath = argv[i]
		case strings.HasPrefix(a, "--config="):
			o.configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-") && a != "-":
			return o, fmt.Errorf("unknown flag %q", a)
		default:
			o.args = append(o.args, a)
		}
	}
	return o, nil
}
