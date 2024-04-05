package tests

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/stretchr/testify/require"
)

func Test_Web3Acceptance(t *testing.T) {
	runTest(t, "foo")
}

// runTest will run the test by name, the name
// must match an existing js test file (without the extension)
func runTest(t *testing.T, name string) {
	stop := servicesSetup(t)
	executeTest(t, name)
	stop()
}

// servicesSetup starts up an emulator and the gateway
// engines required for operation of the evm gateway.
func servicesSetup(t *testing.T) func() {
	srv, err := startEmulator()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	emu := srv.Emulator()
	service := emu.ServiceKey()
	cfg := defaultConfig(t.TempDir(), service.Address, service.PrivateKey)

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()

	time.Sleep(1 * time.Second) // some time to startup
	return func() {
		cancel()
		srv.Stop()
	}
}

// executeTest will run the provided JS test file using mocha
// and will report failure or success of the test.
func executeTest(t *testing.T, testFile string) {
	command := fmt.Sprintf("./web3js/node_modules/.bin/mocha ./web3js/%s.js", testFile)
	parts := strings.Fields(command)

	t.Run(testFile, func(t *testing.T) {
		cmd := exec.Command(parts[0], parts[1:]...)
		if cmd.Err != nil {
			panic(cmd.Err)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				if exitError.ExitCode() == 1 {
					require.Fail(t, string(out))
				}
				t.Fatalf("unknown test issue: %s", err.Error())
			}
		}

		t.Log(string(out))
	})
}
