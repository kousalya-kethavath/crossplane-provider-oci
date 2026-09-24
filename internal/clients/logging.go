/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"io"
	stdlog "log"
)

// configureEmbeddedTerraformLogging suppresses the process-wide standard Go
// logger, which embedded Terraform callbacks use for unredacted request payloads.
// Configure it during setup construction, before reconciliation starts; never
// switch or restore the global writer around individual operations. TF_LOG and
// the controller debug flag must not enable this unfiltered output. Crossplane's
// structured logger and returned Terraform diagnostics are unaffected. This also
// suppresses non-sensitive standard-library log output from other dependencies.
func configureEmbeddedTerraformLogging() {
	stdlog.SetOutput(io.Discard)
}
