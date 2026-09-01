package config

import (
	"flag"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultListenAddress  = "127.0.0.1:9100"
	DefaultMetadataDBPath = "/srv/minibase/data/minibase.db"
)

type Config struct {
	ListenAddress  string
	MetadataDBPath string
}

func Parse(args []string) (Config, error) {
	flags := flag.NewFlagSet("minibase", flag.ContinueOnError)
	listenAddress := flags.String("listen", DefaultListenAddress, "loopback HTTP listen address")
	metadataDBPath := flags.String("metadata-db", DefaultMetadataDBPath, "SQLite metadata database path")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments")
	}
	if err := validateLoopbackAddress(*listenAddress); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(*metadataDBPath) == "" {
		return Config{}, fmt.Errorf("metadata database path must not be empty")
	}

	absoluteDBPath, err := filepath.Abs(filepath.Clean(*metadataDBPath))
	if err != nil {
		return Config{}, fmt.Errorf("resolve metadata database path: %w", err)
	}

	return Config{
		ListenAddress:  *listenAddress,
		MetadataDBPath: absoluteDBPath,
	}, nil
}

func validateLoopbackAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address must use a loopback IP")
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("listen address must use a valid numeric port")
	}

	return nil
}
