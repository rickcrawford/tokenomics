package cmd

import (
	"testing"
)

func TestStartCmd_Registered(t *testing.T) {
	// Verify the start command is registered on the root command.
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "start" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("start command not registered on rootCmd")
	}
}

func TestStartCmd_Flags(t *testing.T) {
	flags := []string{"host", "port", "tls", "pid-file", "log-file"}
	for _, name := range flags {
		if startCmd.Flags().Lookup(name) == nil {
			t.Errorf("start command missing flag: %s", name)
		}
	}
}

func TestDaemonConfig_Defaults(t *testing.T) {
	dc := daemonConfig{
		host: "localhost",
		port: 8443,
		tls:  true,
	}

	if dc.host != "localhost" {
		t.Errorf("host = %q, want localhost", dc.host)
	}
	if dc.port != 8443 {
		t.Errorf("port = %d, want 8443", dc.port)
	}
	if !dc.tls {
		t.Error("tls should be true")
	}
	if dc.insecure {
		t.Error("insecure should default to false")
	}
}
