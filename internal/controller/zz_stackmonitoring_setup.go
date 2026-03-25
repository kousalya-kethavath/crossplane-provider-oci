/*
Copyright 2022 Upbound Inc.
*/

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	baselineablemetric "github.com/oracle/provider-oci/internal/controller/stackmonitoring/baselineablemetric"
	config "github.com/oracle/provider-oci/internal/controller/stackmonitoring/config"
	discoveryjob "github.com/oracle/provider-oci/internal/controller/stackmonitoring/discoveryjob"
	maintenancewindow "github.com/oracle/provider-oci/internal/controller/stackmonitoring/maintenancewindow"
	maintenancewindowsretryfailedoperation "github.com/oracle/provider-oci/internal/controller/stackmonitoring/maintenancewindowsretryfailedoperation"
	maintenancewindowsstop "github.com/oracle/provider-oci/internal/controller/stackmonitoring/maintenancewindowsstop"
	metricextension "github.com/oracle/provider-oci/internal/controller/stackmonitoring/metricextension"
	metricextensionmetricextensionongivenresourcesmanagement "github.com/oracle/provider-oci/internal/controller/stackmonitoring/metricextensionmetricextensionongivenresourcesmanagement"
	monitoredresource "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresource"
	monitoredresourcesassociatemonitoredresource "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourcesassociatemonitoredresource"
	monitoredresourceslistmember "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourceslistmember"
	monitoredresourcessearch "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourcessearch"
	monitoredresourcessearchassociation "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourcessearchassociation"
	monitoredresourcetask "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourcetask"
	monitoredresourcetype "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoredresourcetype"
	monitoringtemplate "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoringtemplate"
	monitoringtemplatealarmcondition "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoringtemplatealarmcondition"
	monitoringtemplateongivenresourcesmanagement "github.com/oracle/provider-oci/internal/controller/stackmonitoring/monitoringtemplateongivenresourcesmanagement"
	processset "github.com/oracle/provider-oci/internal/controller/stackmonitoring/processset"
)

// Setup_stackmonitoring creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup_stackmonitoring(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		baselineablemetric.Setup,
		config.Setup,
		discoveryjob.Setup,
		maintenancewindow.Setup,
		maintenancewindowsretryfailedoperation.Setup,
		maintenancewindowsstop.Setup,
		metricextension.Setup,
		metricextensionmetricextensionongivenresourcesmanagement.Setup,
		monitoredresource.Setup,
		monitoredresourcesassociatemonitoredresource.Setup,
		monitoredresourceslistmember.Setup,
		monitoredresourcessearch.Setup,
		monitoredresourcessearchassociation.Setup,
		monitoredresourcetask.Setup,
		monitoredresourcetype.Setup,
		monitoringtemplate.Setup,
		monitoringtemplatealarmcondition.Setup,
		monitoringtemplateongivenresourcesmanagement.Setup,
		processset.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated_stackmonitoring creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated_stackmonitoring(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		baselineablemetric.SetupGated,
		config.SetupGated,
		discoveryjob.SetupGated,
		maintenancewindow.SetupGated,
		maintenancewindowsretryfailedoperation.SetupGated,
		maintenancewindowsstop.SetupGated,
		metricextension.SetupGated,
		metricextensionmetricextensionongivenresourcesmanagement.SetupGated,
		monitoredresource.SetupGated,
		monitoredresourcesassociatemonitoredresource.SetupGated,
		monitoredresourceslistmember.SetupGated,
		monitoredresourcessearch.SetupGated,
		monitoredresourcessearchassociation.SetupGated,
		monitoredresourcetask.SetupGated,
		monitoredresourcetype.SetupGated,
		monitoringtemplate.SetupGated,
		monitoringtemplatealarmcondition.SetupGated,
		monitoringtemplateongivenresourcesmanagement.SetupGated,
		processset.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}
