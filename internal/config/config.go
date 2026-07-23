// Package config loads sshhh's YAML config and resolves it into a flat list
// of connectable hosts, applying global -> group -> host default cascading.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defaults are settings that cascade from the global level down to groups.
// Pointer/zero-value semantics: an unset field does not override a lower level.
type Defaults struct {
	User     string `yaml:"user"`
	Identity string `yaml:"identity"`
	Jump     string `yaml:"jump"`
	Port     int    `yaml:"port"`
	Sudo     *bool  `yaml:"sudo"`
}

// HostSpec is a single host as written in the config file. The map key is the
// alias, so it is not repeated here.
type HostSpec struct {
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	User     string   `yaml:"user"`
	Identity string   `yaml:"identity"`
	Jump     string   `yaml:"jump"`
	Name     string   `yaml:"name"`
	Desc     string   `yaml:"desc"`
	Tags     []string `yaml:"tags"`
	Sudo     *bool    `yaml:"sudo"`
}

// Group bundles hosts that share defaults (e.g. a common bastion/user).
type Group struct {
	Defaults Defaults            `yaml:"defaults"`
	Hosts    map[string]HostSpec `yaml:"hosts"`
}

// File is the top-level config document.
type File struct {
	Defaults Defaults         `yaml:"defaults"`
	Groups   map[string]Group `yaml:"groups"`
}

// Host is a fully resolved, ready-to-connect entry.
type Host struct {
	Alias    string
	Group    string
	Name     string
	Desc     string
	Tags     []string
	Addr     string
	Port     int
	User     string
	Identity string
	Jump     string
	Sudo     bool
}

// Load reads and resolves the config at path into a sorted host list
// (by group, then alias).
func Load(path string) ([]Host, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var hosts []Host
	for gname, g := range f.Groups {
		gd := mergeDefaults(f.Defaults, g.Defaults)
		for alias, spec := range g.Hosts {
			if spec.Host == "" {
				fmt.Fprintf(os.Stderr, "sshhh: skipping %q in %q: no host address\n", alias, gname)
				continue
			}
			hosts = append(hosts, resolve(gname, alias, spec, gd))
		}
	}

	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Group != hosts[j].Group {
			return hosts[i].Group < hosts[j].Group
		}
		return hosts[i].Alias < hosts[j].Alias
	})
	return hosts, nil
}

func resolve(group, alias string, spec HostSpec, d Defaults) Host {
	h := Host{
		Alias:    alias,
		Group:    group,
		Name:     spec.Name,
		Desc:     spec.Desc,
		Tags:     spec.Tags,
		Addr:     spec.Host,
		Port:     firstNonZero(spec.Port, d.Port, 22),
		User:     firstNonEmpty(spec.User, d.User),
		Identity: expandHome(firstNonEmpty(spec.Identity, d.Identity)),
		Jump:     firstNonEmpty(spec.Jump, d.Jump),
	}
	switch {
	case spec.Sudo != nil:
		h.Sudo = *spec.Sudo
	case d.Sudo != nil:
		h.Sudo = *d.Sudo
	}
	return h
}

func mergeDefaults(base, over Defaults) Defaults {
	if over.User != "" {
		base.User = over.User
	}
	if over.Identity != "" {
		base.Identity = over.Identity
	}
	if over.Jump != "" {
		base.Jump = over.Jump
	}
	if over.Port != 0 {
		base.Port = over.Port
	}
	if over.Sudo != nil {
		base.Sudo = over.Sudo
	}
	return base
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// DefaultPath returns the config location, honoring SSHHH_CONFIG and
// XDG_CONFIG_HOME, defaulting to ~/.config/sshhh/config.yaml.
func DefaultPath() string {
	if p := os.Getenv("SSHHH_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "sshhh", "config.yaml")
}
