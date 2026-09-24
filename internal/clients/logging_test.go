/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"bytes"
	"errors"
	stdlog "log"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestTerraformSetupBuilderSuppressesEmbeddedLogs(t *testing.T) {
	// These tests change the process-wide standard logger and environment.
	// Keep them sequential and restore the writer before another test runs.
	for _, level := range []string{"", "TRACE"} {
		t.Run("TF_LOG="+level, func(t *testing.T) {
			t.Setenv("TF_LOG", level)
			t.Setenv("TF_LOG_PROVIDER", level)
			originalWriter := stdlog.Writer()
			t.Cleanup(func() { stdlog.SetOutput(originalWriter) })
			var standardOutput bytes.Buffer
			stdlog.SetOutput(&standardOutput)

			TerraformSetupBuilder()

			// Exercise both unlevelled payload logs and literal [DEBUG] prefixes
			// used by the embedded provider, including concurrent callbacks.
			var writers sync.WaitGroup
			for range 4 {
				writers.Go(func() {
					stdlog.Printf("parameter_value %s", "synthetic-secret")
					stdlog.Printf("[DEBUG] request payload: %s", "synthetic-secret")
				})
			}
			writers.Wait()
			if standardOutput.Len() != 0 {
				t.Fatal("embedded Terraform payload logs reached the standard logger output")
			}

			// The controller's structured logger uses its own sink. Suppressing
			// the standard logger must leave normal operation errors visible.
			for _, debug := range []bool{false, true} {
				var controllerOutput bytes.Buffer
				logger := logging.NewLogrLogger(zap.New(zap.UseDevMode(debug), zap.WriteTo(&controllerOutput)))
				logger.Info("reconciliation failed", "error", errors.New("operation failed"))
				if !strings.Contains(controllerOutput.String(), "operation failed") {
					t.Fatalf("controller diagnostics were lost with debug=%t", debug)
				}
			}
		})
	}
}
