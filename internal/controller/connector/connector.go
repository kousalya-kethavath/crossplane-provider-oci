/*
Copyright 2026 Oracle and/or its affiliates.
*/

// Package connector provides CLI workspace cleanup for ServiceAccount tokens.
package connector

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewCLIWorkspaceFinalizer removes a CLI workspace and any token files created
// before its registration. In-process controllers retain Upjet's own finalizer.
func NewCLIWorkspaceFinalizer(kube client.Client, store *terraform.WorkspaceStore) xpresource.Finalizer {
	return terraform.NewWorkspaceFinalizer(cliWorkspaceCleaner{store}, xpresource.NewAPIFinalizer(kube, managed.FinalizerName))
}

type cliWorkspaceCleaner struct{ store *terraform.WorkspaceStore }

func (c cliWorkspaceCleaner) Remove(obj xpresource.Object) error {
	uid := string(obj.GetUID())
	if uid == "" || uid == "." || uid == ".." || filepath.Base(uid) != uid {
		return fmt.Errorf("invalid managed resource UID for CLI workspace cleanup")
	}
	if err := c.store.Remove(obj); err != nil {
		return err
	}
	// Setup may have created a token before Upjet registered the workspace
	// (for example, when initialization fails). Finalize that directory too.
	return os.RemoveAll(filepath.Join(os.TempDir(), uid))
}
