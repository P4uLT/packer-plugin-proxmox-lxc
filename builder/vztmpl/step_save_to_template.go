package vztmpl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/Telmate/proxmox-api-go/proxmox"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// stepSaveToTemplate takes the running VM configured in earlier steps, stops it, and
// converts it into a Proxmox template.
//
// It sets the template_id state which is used for Artifact lookup.
type stepSaveToTemplate struct{}

func (s *stepSaveToTemplate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	c := state.Get("config").(*Config)
	client := state.Get("proxmoxClient").(*proxmox.Client)
	vmRef := state.Get("vmRef").(*proxmox.VmRef)

	backupSrcPath := state.Get("backupSrcPath").(string)
	extension := state.Get("extension").(string)

	user, err := proxmox.NewUserID(c.Username)
	if err != nil {
		err := fmt.Errorf("error parsing username: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Define new name
	baseName := fileNameWithoutExtension(c.TemplateFile)
	templateDstName := fmt.Sprintf("%s_%s.%s", baseName, c.TemplateSuffix, extension)

	ui.Say(fmt.Sprintf("Establishing SSH connection at [%s] to get backup...", c.proxmoxURL.Hostname()))
	SftpClient, err := ConnectSFTP(ui, user.Name, c.Password, c.proxmoxURL.Hostname(), 22)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("Establishing SSH connection at [%s] to get backup...Done", c.proxmoxURL.Hostname()))
	// open an SFTP session over an existing ssh connection.
	err = uploadBackup(*client, ui, SftpClient, c.VMID, c.Node, backupSrcPath, c.TemplateStoragePool, templateDstName)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	defer SftpClient.Close()

	ui.Say("Finished. Deleting Backup File")
	err = SftpClient.Remove(backupSrcPath)
	if err != nil {
		ui.Error(fmt.Sprintf("Error Backup. Please delete it manually: %s", err))

	}
	ui.Say("Finished. Deleting Backup File...Done")

	ui.Say("Finished. Deleting LXC Container")
	_, err = client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM. Please delete it manually: %s", err))
	}
	ui.Say("Finished. Deleting LXC Container... Done")

	state.Put("templatePath", templateDstName)

	return multistep.ActionContinue
}

func (s *stepSaveToTemplate) Cleanup(state multistep.StateBag) {

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
	ui := state.Get("ui").(packersdk.Ui)

	ui.Say("Deleting LXC Container")
	_, err := client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM. Please delete it manually: %s", err))
		return
	}
	ui.Say("Deleting LXC Container... Done")

}

func ConnectSFTP(ui packersdk.Ui, apiUser string, apiPassword string, apiAddr string, apiPort int) (*sftp.Client, error) {

	config := &ssh.ClientConfig{
		User: apiUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(apiPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var sshAddr string = apiAddr + ":" + strconv.Itoa(apiPort)

	client, _ := ssh.Dial("tcp", sshAddr, config)
	ftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}

	return ftpClient, nil
}

func uploadBackup(prox_client proxmox.Client, ui packersdk.Ui, ftpClient *sftp.Client, vmId int, node string, srcFilePath string, templateStoragePool string, templateDstName string) error {

	ui.Say(fmt.Sprintf("Opening vzdump template backup %s ...", srcFilePath))
	srcFile, err := ftpClient.Open(srcFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	ui.Say("Creating temp file to store backup file ...")
	dstFile, err := os.CreateTemp("", "vztmpl")
	if err != nil {
		return err
	}

	ui.Say("Transferring vzdump data template to local path...")
	// write to file
	if _, err := dstFile.ReadFrom(srcFile); err != nil {
		return err
	}
	defer os.Remove(dstFile.Name())

	ui.Say(fmt.Sprintf("Upload template %s to %s...", templateDstName, templateStoragePool))
	isoPath, _ := filepath.EvalSymlinks(dstFile.Name())
	r, err := os.Open(isoPath)
	if err != nil {
		return err
	}
	if err := prox_client.Upload(node, templateStoragePool, "vztmpl", templateDstName, r); err != nil {
		return err
	}

	return nil
}

func fileNameWithoutExtension(fileName string) string {
	i := 0
	for i < 2 {
		fileName = strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
		i = i + 1
	}
	return fileName
}
