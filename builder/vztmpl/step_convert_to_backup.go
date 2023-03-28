package vztmpl

import (
	"context"
	"crypto/tls"
	"fmt"
	"regexp"
	"sort"
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

// ByCreationTime implements sort.Interface based on the CreationTime field.
type ByCreationTime []proxmox.Content_FileProperties

func (a ByCreationTime) Len() int           { return len(a) }
func (a ByCreationTime) Less(i, j int) bool { return a[i].CreationTime.After(a[j].CreationTime) }
func (a ByCreationTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

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

	ui.Say("Converting LXC Container to backup")

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
	params["compress"] = "gzip" //TODO: check template to determine which compress to apply
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

	ui.Say(fmt.Sprintf("Finding latest backup for VmId %d in storage :%s", vmRef.VmId(), c.BackupStoragePool))
	backupSrcPath, extension, err := findLatestBackup(client, c.VMID, c.Node, c.BackupStoragePool)

	if err != nil {
		err := fmt.Errorf("error finding latest backup: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Found backup at " + backupSrcPath)

	state.Put("backupSrcPath", backupSrcPath)
	state.Put("extension", extension)

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

func findLatestBackup(proxmox_client *proxmox.Client, vmId int, node string, storagePool string) (string, string, error) {
	// Get Files List
	contentList, err := proxmox.ListFiles(proxmox_client, node, storagePool, proxmox.ContentType_Backup)
	if err != nil {
		return "", "", err
	}

	if len(*contentList) == 0 {
		return "", "", fmt.Errorf("could not find backup file for LXC container %d", vmId)
	}

	var current_vmRefs_backup []proxmox.Content_FileProperties
	// Apply regex to find all with the right vmRef
	for _, file := range *contentList {

		if isCurrentvmRef(vmId, file) {
			current_vmRefs_backup = append(current_vmRefs_backup, file)
		}
	}
	// Sorting by date desc
	sort.Sort(ByCreationTime(current_vmRefs_backup))

	current_backup := current_vmRefs_backup[0]

	url := fmt.Sprintf("/nodes/%s/storage/%s/content/%s:%s/%s", node, storagePool, storagePool, string(proxmox.ContentType_Backup), current_backup.Name)
	filedetail, err := proxmox_client.GetItemConfigMapStringInterface(url, "list_storage", "STORAGE")

	//filedetail, err := proxmox_client.GetItemConfigMapStringInterface("/nodes/"+node+"/storage/"+storagePool+"/content/"+storagePool+":"+string(proxmox.ContentType_Backup)+"/"+current_backup.Name, "PATH", "CONFIG")
	if err != nil {
		return "", "", nil
	}
	srcFilePath := filedetail["path"].(string)
	if srcFilePath == "" {
		return "", "", fmt.Errorf("could not find backup file for LXC container %d", vmId)
	}

	return srcFilePath, current_backup.Format, nil
}

func isCurrentvmRef(vmId int, file proxmox.Content_FileProperties) bool {

	match, err := regexp.MatchString(fmt.Sprintf(`vzdump-lxc-%d-.*?\.tar\.gz`, vmId), file.Name)
	if err != nil {
		return false
	}
	return match
}
