package vztmpl

import (
	"fmt"
	"log"
	"os"

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
	return a.StateData[name]
}

func (a *Artifact) Destroy() error {
	log.Printf("Destroying template: %s", a.templatePath)
	err := os.Remove(a.templatePath)
	return err
}
