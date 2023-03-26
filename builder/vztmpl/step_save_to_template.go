package vztmpl

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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

	filename, err := uploadBackup(*client, ui, strings.Replace(c.Username, "@pam", "", 1), c.Password, c.proxmoxURL.Hostname(), 22, c.VMID, c.Node, c.TemplateStoragePool, c.TemplateFile)
	if err != nil {
		err := fmt.Errorf("error converting VM to template, failed to upload backup: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Finished. Deleting LXC Container")
	_, err = client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("error deleting VM. Please delete it manually: %s", err))
	}

	state.Put("templatePath", filename)

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

}

func uploadBackup(prox_client proxmox.Client, ui packersdk.Ui, apiUser string, apiPassword string, apiAddr string, apiPort int, vmId int, node string, templateStoragePool string, templateFile string) (string, error) {

	config := &ssh.ClientConfig{
		User: apiUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(apiPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var sshAddr string = apiAddr + ":" + strconv.Itoa(apiPort)
	ui.Say("Establishing SSH connection with [" + apiUser + "] at [" + sshAddr + "] for template file...")
	client, _ := ssh.Dial("tcp", sshAddr, config)
	defer client.Close()

	ui.Say("Establishing SFTP connection for template file...")
	// open an SFTP session over an existing ssh connection.
	ftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", err
	}
	defer ftpClient.Close()

	ui.Say("Listing vzdump backup directory for template backup...")
	dir := "/var/lib/vz/dump/"
	files, err := ftpClient.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var srcFilePath = ""
	var fileName = ""
	for _, file := range files {
		match, err := regexp.MatchString(`vzdump-lxc-`+strconv.Itoa(vmId)+`-.*?\.tar\.gz`, file.Name())
		if err == nil && match {
			fileName = file.Name()
			srcFilePath = path.Join(dir, fileName)
		}
	}

	if srcFilePath == "" {
		return "", fmt.Errorf("could not find backup file for LXC container %d", vmId)
	}

	ui.Say("Opening vzdump template backup " + srcFilePath + "...")
	srcFile, err := ftpClient.Open(srcFilePath)
	if err != nil {
		return "", err
	}
	defer srcFile.Close()

	dstPath := "./out/" + fileName

	ui.Say("Creating local template file " + dstPath + "...")
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}

	defer dstFile.Close()

	ui.Say("Transferring vzdump template backup file to local path...")
	// write to file
	if _, err := dstFile.ReadFrom(srcFile); err != nil {
		return "", err
	}

	extension := filepath.Ext(templateFile)
	name := templateFile[0 : len(templateFile)-len(extension)]

	templateFile = name + "_updated" + extension

	fmt.Println(templateFile)

	ui.Say("Upload vzdump template backup " + templateFile + " to " + templateStoragePool + " ...")
	isoPath, _ := filepath.EvalSymlinks(dstPath)
	r, err := os.Open(isoPath)
	if err != nil {
		return "", err
	}
	if err := prox_client.Upload(node, templateStoragePool, "vztmpl", dstPath, r); err != nil {
		return "", err
	}

	return templateFile, nil
}

func fileNameWithoutExtension(fileName string) string {
	if pos := strings.LastIndexByte(fileName, '.'); pos != -1 {
		return fileName[:pos]
	}
	return fileName
}

func downloadBackup(ui packersdk.Ui, apiUser string, apiPassword string, apiAddr string, apiPort int, vmId int, dstPath string) error {
	config := &ssh.ClientConfig{
		User: apiUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(apiPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var sshAddr string = apiAddr + ":" + strconv.Itoa(apiPort)
	ui.Say("Establishing SSH connection with [" + apiUser + "] at [" + sshAddr + "] for template file...")
	client, _ := ssh.Dial("tcp", sshAddr, config)
	defer client.Close()

	ui.Say("Establishing SFTP connection for template file...")
	// open an SFTP session over an existing ssh connection.
	ftpClient, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer ftpClient.Close()

	ui.Say("Listing vzdump backup directory for template backup...")
	dir := "/var/lib/vz/dump/"
	files, err := ftpClient.ReadDir(dir)
	if err != nil {
		return err
	}
	var srcFilePath = ""
	for _, file := range files {
		match, err := regexp.MatchString(`vzdump-lxc-`+strconv.Itoa(vmId)+`-.*?\.tar\.gz`, file.Name())
		if err == nil && match {
			srcFilePath = path.Join(dir, file.Name())
		}
	}

	if srcFilePath == "" {
		return fmt.Errorf("could not find backup file for LXC container %d", vmId)
	}

	ui.Say("Opening vzdump template backup " + srcFilePath + "...")
	srcFile, err := ftpClient.Open(srcFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	ui.Say("Creating local template file " + dstPath + "...")
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	ui.Say("Transferring vzdump template backup file to local path...")
	// write to file
	if _, err := dstFile.ReadFrom(srcFile); err != nil {
		return err
	}

	return nil
}
