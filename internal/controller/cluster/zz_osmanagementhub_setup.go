/*
Copyright 2022 Upbound Inc.
*/

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	event "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/event"
	lifecycleenvironment "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/lifecycleenvironment"
	lifecyclestageattachmanagedinstancesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/lifecyclestageattachmanagedinstancesmanagement"
	lifecyclestagedetachmanagedinstancesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/lifecyclestagedetachmanagedinstancesmanagement"
	lifecyclestagepromotesoftwaresourcemanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/lifecyclestagepromotesoftwaresourcemanagement"
	lifecyclestagerebootmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/lifecyclestagerebootmanagement"
	managedinstance "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstance"
	managedinstanceattachprofilemanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstanceattachprofilemanagement"
	managedinstancedetachprofilemanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancedetachprofilemanagement"
	managedinstancegroup "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroup"
	managedinstancegroupattachmanagedinstancesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupattachmanagedinstancesmanagement"
	managedinstancegroupattachsoftwaresourcesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupattachsoftwaresourcesmanagement"
	managedinstancegroupdetachmanagedinstancesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupdetachmanagedinstancesmanagement"
	managedinstancegroupdetachsoftwaresourcesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupdetachsoftwaresourcesmanagement"
	managedinstancegroupinstallpackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupinstallpackagesmanagement"
	managedinstancegroupinstallwindowsupdatesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupinstallwindowsupdatesmanagement"
	managedinstancegroupmanagemodulestreamsmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupmanagemodulestreamsmanagement"
	managedinstancegrouprebootmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegrouprebootmanagement"
	managedinstancegroupremovepackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupremovepackagesmanagement"
	managedinstancegroupupdateallpackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancegroupupdateallpackagesmanagement"
	managedinstanceinstallwindowsupdatesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstanceinstallwindowsupdatesmanagement"
	managedinstancerebootmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstancerebootmanagement"
	managedinstanceupdatepackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managedinstanceupdatepackagesmanagement"
	managementstation "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managementstation"
	managementstationassociatemanagedinstancesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managementstationassociatemanagedinstancesmanagement"
	managementstationmirrorsynchronizemanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managementstationmirrorsynchronizemanagement"
	managementstationrefreshmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managementstationrefreshmanagement"
	managementstationsynchronizemirrorsmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/managementstationsynchronizemirrorsmanagement"
	profile "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profile"
	profileattachlifecyclestagemanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profileattachlifecyclestagemanagement"
	profileattachmanagedinstancegroupmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profileattachmanagedinstancegroupmanagement"
	profileattachmanagementstationmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profileattachmanagementstationmanagement"
	profileattachsoftwaresourcesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profileattachsoftwaresourcesmanagement"
	profiledetachsoftwaresourcesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/profiledetachsoftwaresourcesmanagement"
	scheduledjob "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/scheduledjob"
	softwaresource "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresource"
	softwaresourceaddpackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourceaddpackagesmanagement"
	softwaresourcechangeavailabilitymanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourcechangeavailabilitymanagement"
	softwaresourcegeneratemetadatamanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourcegeneratemetadatamanagement"
	softwaresourcemanifest "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourcemanifest"
	softwaresourceremovepackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourceremovepackagesmanagement"
	softwaresourcereplacepackagesmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/softwaresourcereplacepackagesmanagement"
	workrequestrerunmanagement "github.com/oracle/provider-oci/internal/controller/cluster/osmanagementhub/workrequestrerunmanagement"
)

// Setup_osmanagementhub creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup_osmanagementhub(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		event.Setup,
		lifecycleenvironment.Setup,
		lifecyclestageattachmanagedinstancesmanagement.Setup,
		lifecyclestagedetachmanagedinstancesmanagement.Setup,
		lifecyclestagepromotesoftwaresourcemanagement.Setup,
		lifecyclestagerebootmanagement.Setup,
		managedinstance.Setup,
		managedinstanceattachprofilemanagement.Setup,
		managedinstancedetachprofilemanagement.Setup,
		managedinstancegroup.Setup,
		managedinstancegroupattachmanagedinstancesmanagement.Setup,
		managedinstancegroupattachsoftwaresourcesmanagement.Setup,
		managedinstancegroupdetachmanagedinstancesmanagement.Setup,
		managedinstancegroupdetachsoftwaresourcesmanagement.Setup,
		managedinstancegroupinstallpackagesmanagement.Setup,
		managedinstancegroupinstallwindowsupdatesmanagement.Setup,
		managedinstancegroupmanagemodulestreamsmanagement.Setup,
		managedinstancegrouprebootmanagement.Setup,
		managedinstancegroupremovepackagesmanagement.Setup,
		managedinstancegroupupdateallpackagesmanagement.Setup,
		managedinstanceinstallwindowsupdatesmanagement.Setup,
		managedinstancerebootmanagement.Setup,
		managedinstanceupdatepackagesmanagement.Setup,
		managementstation.Setup,
		managementstationassociatemanagedinstancesmanagement.Setup,
		managementstationmirrorsynchronizemanagement.Setup,
		managementstationrefreshmanagement.Setup,
		managementstationsynchronizemirrorsmanagement.Setup,
		profile.Setup,
		profileattachlifecyclestagemanagement.Setup,
		profileattachmanagedinstancegroupmanagement.Setup,
		profileattachmanagementstationmanagement.Setup,
		profileattachsoftwaresourcesmanagement.Setup,
		profiledetachsoftwaresourcesmanagement.Setup,
		scheduledjob.Setup,
		softwaresource.Setup,
		softwaresourceaddpackagesmanagement.Setup,
		softwaresourcechangeavailabilitymanagement.Setup,
		softwaresourcegeneratemetadatamanagement.Setup,
		softwaresourcemanifest.Setup,
		softwaresourceremovepackagesmanagement.Setup,
		softwaresourcereplacepackagesmanagement.Setup,
		workrequestrerunmanagement.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated_osmanagementhub creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated_osmanagementhub(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		event.SetupGated,
		lifecycleenvironment.SetupGated,
		lifecyclestageattachmanagedinstancesmanagement.SetupGated,
		lifecyclestagedetachmanagedinstancesmanagement.SetupGated,
		lifecyclestagepromotesoftwaresourcemanagement.SetupGated,
		lifecyclestagerebootmanagement.SetupGated,
		managedinstance.SetupGated,
		managedinstanceattachprofilemanagement.SetupGated,
		managedinstancedetachprofilemanagement.SetupGated,
		managedinstancegroup.SetupGated,
		managedinstancegroupattachmanagedinstancesmanagement.SetupGated,
		managedinstancegroupattachsoftwaresourcesmanagement.SetupGated,
		managedinstancegroupdetachmanagedinstancesmanagement.SetupGated,
		managedinstancegroupdetachsoftwaresourcesmanagement.SetupGated,
		managedinstancegroupinstallpackagesmanagement.SetupGated,
		managedinstancegroupinstallwindowsupdatesmanagement.SetupGated,
		managedinstancegroupmanagemodulestreamsmanagement.SetupGated,
		managedinstancegrouprebootmanagement.SetupGated,
		managedinstancegroupremovepackagesmanagement.SetupGated,
		managedinstancegroupupdateallpackagesmanagement.SetupGated,
		managedinstanceinstallwindowsupdatesmanagement.SetupGated,
		managedinstancerebootmanagement.SetupGated,
		managedinstanceupdatepackagesmanagement.SetupGated,
		managementstation.SetupGated,
		managementstationassociatemanagedinstancesmanagement.SetupGated,
		managementstationmirrorsynchronizemanagement.SetupGated,
		managementstationrefreshmanagement.SetupGated,
		managementstationsynchronizemirrorsmanagement.SetupGated,
		profile.SetupGated,
		profileattachlifecyclestagemanagement.SetupGated,
		profileattachmanagedinstancegroupmanagement.SetupGated,
		profileattachmanagementstationmanagement.SetupGated,
		profileattachsoftwaresourcesmanagement.SetupGated,
		profiledetachsoftwaresourcesmanagement.SetupGated,
		scheduledjob.SetupGated,
		softwaresource.SetupGated,
		softwaresourceaddpackagesmanagement.SetupGated,
		softwaresourcechangeavailabilitymanagement.SetupGated,
		softwaresourcegeneratemetadatamanagement.SetupGated,
		softwaresourcemanifest.SetupGated,
		softwaresourceremovepackagesmanagement.SetupGated,
		softwaresourcereplacepackagesmanagement.SetupGated,
		workrequestrerunmanagement.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}
