package vztmpl

import (
	"fmt"
	"log"

	"github.com/Telmate/proxmox-api-go/proxmox"
)

// packersdk.Artifact implementation
type Artifact struct {
	templatePath  string
	proxmoxClient *proxmox.Client

	// StateData should store data such as GeneratedData
	// to be shared with post-processors
	StateData map[string]interface{}
}

func (*Artifact) BuilderId() string {
	return BuilderId
}

func (a *Artifact) Files() []string {
	return []string{a.templatePath}
}

func (a *Artifact) Id() string {
	return a.templatePath
}

func (a *Artifact) String() string {
	return fmt.Sprintf("A template was created: %s", a.templatePath)
}

func (a *Artifact) State(name string) interface{} {
	if _, ok := a.StateData[name]; ok {
		return a.StateData[name]
	}
	return nil
}

func (a *Artifact) Destroy() error {
	log.Printf("Destroying template: %s", a.templatePath)
	return nil
}
