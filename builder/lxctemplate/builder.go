package lxctemplate

import (
	"context"
	"errors"
	"fmt"

	"github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

const BuilderId = "proxmox-lxc.builder"

type Builder struct {
	config        Config
	runner        multistep.Runner
	proxmoxClient *proxmox.Client
}

// Builder implements packer.Builder
var _ packersdk.Builder = &Builder{}

func (b *Builder) ConfigSpec() hcldec.ObjectSpec { return b.config.FlatMapstructure().HCL2Spec() }

func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	warnings, errs := b.config.Prepare(raws...)
	if errs != nil {
		return nil, warnings, errs
	}
	// Return the placeholder for the generated data that will become available to provisioners and post-processors.
	// If the builder doesn't generate any data, just return an empty slice of string: []string{}
	buildGeneratedData := []string{"GeneratedMockData"}
	return buildGeneratedData, nil, nil
}

func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {

	// Handle Proxmox connection
	var err error
	b.proxmoxClient, err = newProxmoxClient(b.config)
	if err != nil {
		return nil, err
	}

	// Setup the state bag and initial state for the steps
	state := new(multistep.BasicStateBag)
	state.Put("config", &b.config)
	state.Put("proxmoxClient", b.proxmoxClient)
	state.Put("hook", hook)
	state.Put("ui", ui)

	comm := &b.config.Comm

	// Build the steps
	var steps []multistep.Step

	steps = append(steps,

		&StepSshKeyPair{},
		&stepStartContainer{},
		commonsteps.HTTPServerFromHTTPConfig(&b.config.HTTPConfig),
		&communicator.StepConnect{
			Config:    &b.config.Comm,
			Host:      commHost((*comm).Host()),
			SSHConfig: (*comm).SSHConfigFunc(),
		},
		&commonsteps.StepProvision{},
		&commonsteps.StepCleanupTempKeys{
			Comm: &b.config.Comm,
		},
		&stepConvertToBackup{},
		&stepSaveToTemplate{},
		&stepSuccess{})

	// Set the value of the generated data that will become available to provisioners.
	// To share the data with post-processors, use the StateData in the artifact.
	state.Put("generated_data", map[string]interface{}{
		"GeneratedMockData": "mock-build-data",
	})

	// Run!
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// If there was an error, return that
	if err, ok := state.GetOk("error"); ok {
		return nil, err.(error)
	}

	// If we were interrupted or cancelled, then just exit.
	if _, ok := state.GetOk(multistep.StateCancelled); ok {
		return nil, errors.New("build was cancelled")
	}

	// Verify that the templatePath was set properly, otherwise we didn't progress through the last step
	templatePath, ok := state.Get("templatePath").(string)
	if !ok {
		return nil, fmt.Errorf("template ID could not be determined")
	}

	artifact := &Artifact{
		// Add the builder generated data to the artifact StateData so that post-processors
		// can access them.
		StateData:    map[string]interface{}{"generated_data": state.Get("generated_data")},
		templatePath: templatePath,
	}
	return artifact, nil
}

// Returns ssh_host or winrm_host (see communicator.Config.Host) config
// parameter when set, otherwise gets the host IP from running VM
func commHost(host string) func(state multistep.StateBag) (string, error) {
	if host != "" {
		return func(state multistep.StateBag) (string, error) {
			return host, nil
		}
	}
	return getVMIP
}

// Reads the first non-loopback interface's IP address from the VM.
// qemu-guest-agent package must be installed on the VM
func getVMIP(state multistep.StateBag) (string, error) {
	client := state.Get("proxmoxClient").(*proxmox.Client)
	vmRef := state.Get("vmRef").(*proxmox.VmRef)

	ifs, err := client.GetVmAgentNetworkInterfaces(vmRef)
	if err != nil {
		return "", err
	}

	for _, iface := range ifs {
		for _, addr := range iface.IPAddresses {
			if addr.IsLoopback() {
				continue
			}
			return addr.String(), nil
		}
	}

	return "", fmt.Errorf("found no IP addresses on VM")
}
