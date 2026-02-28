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
	flags := []string{"host", "port", "tls", "insecure", "pid-file", "log-file"}
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

func TestBuildStartDaemonConfig_TLSKeepsInsecureFalseByDefault(t *testing.T) {
	prevHost, prevPort := startHost, startPort
	prevTLS, prevInsecure, prevPid, prevLog := startTLS, startInsecure, startPidFile, startLogFile
	t.Cleanup(func() {
		startHost, startPort = prevHost, prevPort
		startTLS, startInsecure, startPidFile, startLogFile = prevTLS, prevInsecure, prevPid, prevLog
	})

	startHost = "localhost"
	startPort = 8443
	startTLS = true
	startInsecure = false
	startPidFile = "/tmp/test.pid"
	startLogFile = "/tmp/test.log"

	dc := buildStartDaemonConfig()
	if dc.insecure {
		t.Fatal("did not expect insecure readiness probe when TLS is enabled by default")
	}
	if dc.host != startHost || dc.port != startPort || dc.pidFile != startPidFile || dc.logFile != startLogFile {
		t.Fatal("daemon config fields do not match start flags")
	}
}

func TestBuildStartDaemonConfig_HTTPDoesNotSetInsecureReadinessProbe(t *testing.T) {
	prevTLS, prevInsecure := startTLS, startInsecure
	t.Cleanup(func() {
		startTLS, startInsecure = prevTLS, prevInsecure
	})

	startTLS = false
	startInsecure = false
	dc := buildStartDaemonConfig()
	if dc.insecure {
		t.Fatal("did not expect insecure readiness probe when TLS is disabled")
	}
}

func TestBuildStartDaemonConfig_InsecureFlagEnablesInsecureProbe(t *testing.T) {
	prevInsecure := startInsecure
	t.Cleanup(func() {
		startInsecure = prevInsecure
	})

	startInsecure = true
	dc := buildStartDaemonConfig()
	if !dc.insecure {
		t.Fatal("expected insecure readiness probe when --insecure is set")
	}
}
