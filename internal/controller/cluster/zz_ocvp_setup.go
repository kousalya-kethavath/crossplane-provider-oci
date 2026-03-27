/*
Copyright 2022 Upbound Inc.
*/

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	cluster "github.com/oracle/provider-oci/internal/controller/cluster/ocvp/cluster"
	datastore "github.com/oracle/provider-oci/internal/controller/cluster/ocvp/datastore"
	datastorecluster "github.com/oracle/provider-oci/internal/controller/cluster/ocvp/datastorecluster"
	esxihost "github.com/oracle/provider-oci/internal/controller/cluster/ocvp/esxihost"
	sddc "github.com/oracle/provider-oci/internal/controller/cluster/ocvp/sddc"
)

// Setup_ocvp creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup_ocvp(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		cluster.Setup,
		datastore.Setup,
		datastorecluster.Setup,
		esxihost.Setup,
		sddc.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated_ocvp creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated_ocvp(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		cluster.SetupGated,
		datastore.SetupGated,
		datastorecluster.SetupGated,
		esxihost.SetupGated,
		sddc.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}
