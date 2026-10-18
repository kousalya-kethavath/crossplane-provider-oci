/*
Copyright 2026 Oracle and/or its affiliates.
*/

// Package connector selects the existing Upjet CLI or in-process connector.
package connector

import (
	"fmt"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	tjcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/controller/handler"
	"github.com/crossplane/upjet/v2/pkg/metrics"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"github.com/oracle/provider-oci/internal/features"
	"os"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewAsyncConnector uses the CLI workspace when present; otherwise it preserves
// the existing in-process SDKv2 connector and its options.
func NewAsyncConnector(mgr ctrl.Manager, o tjcontroller.Options, cfg *config.Resource, events *handler.EventHandler, callbacks tjcontroller.CallbackProvider, recorder *metrics.MetricRecorder) managed.ExternalConnecter {
	if o.WorkspaceStore != nil {
		return tjcontroller.NewConnector(mgr.GetClient(), o.WorkspaceStore, o.SetupFn, cfg,
			tjcontroller.WithLogger(o.Logger), tjcontroller.WithConnectorEventHandler(events), tjcontroller.WithCallbackProvider(callbacks))
	}
	return tjcontroller.NewTerraformPluginSDKAsyncConnector(mgr.GetClient(), o.OperationTrackerStore, o.SetupFn, cfg,
		tjcontroller.WithTerraformPluginSDKAsyncLogger(o.Logger),
		tjcontroller.WithTerraformPluginSDKAsyncConnectorEventHandler(events),
		tjcontroller.WithTerraformPluginSDKAsyncCallbackProvider(callbacks),
		tjcontroller.WithTerraformPluginSDKAsyncMetricRecorder(recorder),
		tjcontroller.WithTerraformPluginSDKAsyncManagementPolicies(o.Features.Enabled(features.EnableBetaManagementPolicies)))
}

// NewFinalizer pairs each execution mode with its matching resource cleanup.
func NewFinalizer(kube client.Client, o tjcontroller.Options) xpresource.Finalizer {
	api := xpresource.NewAPIFinalizer(kube, managed.FinalizerName)
	if o.WorkspaceStore != nil {
		return terraform.NewWorkspaceFinalizer(cliWorkspaceCleaner{o.WorkspaceStore}, api)
	}
	return tjcontroller.NewOperationTrackerFinalizer(o.OperationTrackerStore, api)
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
