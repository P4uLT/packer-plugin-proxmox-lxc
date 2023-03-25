package main

import (
	"fmt"
	"os"
	lxctemplate "packer-plugin-proxmox-lxc/builder/lxctemplate"
	lxctemplateVersion "packer-plugin-proxmox-lxc/version"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder("lxctemplate", new(lxctemplate.Builder))
	pps.SetVersion(lxctemplateVersion.PluginVersion)
	err := pps.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
