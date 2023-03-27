package main

import (
	"fmt"
	"os"
	vztmpl "packer-plugin-proxmox-lxc/builder/vztmpl"
	proxmoxlxcVersion "packer-plugin-proxmox-lxc/version"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder("vztmpl", new(vztmpl.Builder))
	pps.SetVersion(proxmoxlxcVersion.PluginVersion)
	err := pps.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
