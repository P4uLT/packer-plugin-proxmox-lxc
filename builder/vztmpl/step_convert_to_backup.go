package vztmpl

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/Telmate/proxmox-api-go/proxmox"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// stepConvertToBackup takes the running VM configured in earlier steps, stops it, and
// converts it into a Proxmox template.
//
// It sets the template_id state which is used for Artifact lookup.
type stepConvertToBackup struct{}

func (s *stepConvertToBackup) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	c := state.Get("config").(*Config)
	client := state.Get("proxmoxClient").(*proxmox.Client)
	vmRef := state.Get("vmRef").(*proxmox.VmRef)

	ui.Say("Stopping LXC Container")
	_, err := client.ShutdownVm(vmRef)
	if err != nil {
		err := fmt.Errorf("error converting VM to template, could not stop: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Converting LXC Container to template")

	tlsConf := &tls.Config{InsecureSkipVerify: true}
	session, _ := proxmox.NewSession(c.proxmoxURL.String(), nil, "", tlsConf)
	err = session.Login(c.Username, c.Password, "")
	if err != nil {
		err := fmt.Errorf("error converting VM to template, failed to create session: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	params := make(map[string]interface{})
	params["mode"] = "stop"
	params["compress"] = "gzip" //"zstd"
	params["remove"] = "1"
	params["storage"] = c.BackupStoragePool
	params["vmid"] = strconv.Itoa(c.VMID)

	_, err = client.VzDump(vmRef, params)
	if err != nil {
		err := fmt.Errorf("error converting VM to template, failed to wait process completion: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	return multistep.ActionContinue
}

func (s *stepConvertToBackup) Cleanup(state multistep.StateBag) {

	vmRefUntyped, ok := state.GetOk("vmRef")
	// If not ok, we probably errored out before creating the VM
	if !ok {
		return
	}
	vmRef := vmRefUntyped.(*proxmox.VmRef)

	// The vmRef will actually refer to the created template if everything
	// finished successfully, so in that case we shouldn't cleanup
	if _, ok := state.GetOk("success"); ok {
		return
	}

	client := state.Get("proxmoxClient").(startedVMCleaner)

	err := client.CheckVmRef(vmRef)
	if err != nil {
		return 
	}

	ui := state.Get("ui").(packersdk.Ui)

	// Destroy the server we just created
	ui.Say("Stopping LXC Container")
	_, err = client.StopVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error stopping VM. Please stop and delete it manually: %s", err))
		return
	}

	ui.Say("Deleting LXC Container")
	_, err = client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM. Please delete it manually: %s", err))
		return
	}

}
