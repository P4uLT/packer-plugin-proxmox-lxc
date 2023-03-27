//go:generate packer-sdc struct-markdown

//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package vztmpl

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"github.com/mitchellh/mapstructure"
)

type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	commonsteps.HTTPConfig `mapstructure:",squash"`
	bootcommand.BootConfig `mapstructure:",squash"`
	BootKeyInterval        time.Duration       `mapstructure:"boot_key_interval"`
	Comm                   communicator.Config `mapstructure:",squash"`

	ProxmoxURLRaw      string `mapstructure:"proxmox_url"`
	proxmoxURL         *url.URL
	SkipCertValidation bool          `mapstructure:"insecure_skip_tls_verify"`
	Username           string        `mapstructure:"username"`
	Password           string        `mapstructure:"password"`
	Token              string        `mapstructure:"token"`
	Node               string        `mapstructure:"node"`
	Pool               string        `mapstructure:"pool"`
	TaskTimeout        time.Duration `mapstructure:"task_timeout"`

	Memory         int    `mapstructure:"memory"`
	Cores          int    `mapstructure:"cores"`
	Unprivileged   bool   `mapstructure:"unprivileged"`
	TemplateFile   string `mapstructure:"template_file"`
	TemplateSuffix string `mapstructure:"template_suffix"`

	TemplateStoragePool string `mapstructure:"template_storage_pool"`
	BackupStoragePool   string `mapstructure:"backup_storage_pool"`
	FSStorage           string `mapstructure:"filesystem_storage"`
	FSSize              int    `mapstructure:"filesystem_size"`
	VMID                int    `mapstructure:"vmid"`

	ProvisionIP        string `mapstructure:"provision_ip"`
	ProvisionGatewayIP string `mapstructure:"provision_gateway_ip"`
	ProvisionMac       string `mapstructure:"provision_mac"`

	ctx interpolate.Context
}

func (c *Config) Prepare(raws ...interface{}) ([]string, error) {
	var md mapstructure.Metadata
	err := config.Decode(c, &config.DecodeOpts{
		Metadata:           &md,
		PluginType:         "packer.builder.proxmox-lxc",
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{
				"boot_command",
			},
		},
	}, raws...)
	if err != nil {
		return nil, err
	}

	var errs *packer.MultiError
	// Defaults
	if c.ProxmoxURLRaw == "" {
		c.ProxmoxURLRaw = os.Getenv("PROXMOX_URL")
	}
	if c.Username == "" {
		c.Username = os.Getenv("PROXMOX_USERNAME")
	}
	if c.Password == "" {
		c.Password = os.Getenv("PROXMOX_PASSWORD")
	}
	if c.Token == "" {
		c.Token = os.Getenv("PROXMOX_TOKEN")
	}
	if c.TaskTimeout == 0 {
		c.TaskTimeout = 60 * time.Second
	}
	if c.Memory < 16 {
		log.Printf("Memory %d is too small, using default: 512", c.Memory)
		c.Memory = 512
	}
	if c.Cores < 1 {
		log.Printf("Number of cores %d is too small, using default: 1", c.Cores)
		c.Cores = 1
	}

	if c.ProvisionMac == "" {
		c.ProvisionMac = "1e:eb:08:d1:e7:e2"
	}

	if c.TemplateStoragePool == "" {
		c.TemplateStoragePool = "local"
	}

	// Required configurations that will display errors if not set
	if c.Username == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("username must be specified"))
	}
	if c.Password == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("password must be specified"))
	}
	if c.ProxmoxURLRaw == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("proxmox_url must be specified"))
	}
	if c.proxmoxURL, err = url.Parse(c.ProxmoxURLRaw); err != nil {
		errs = packer.MultiErrorAppend(errs, fmt.Errorf("could not parse proxmox_url: %s", err))
	}
	if c.Node == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("node must be specified"))
	}
	if strings.ContainsAny(c.TemplateFile, " ") {
		errs = packer.MultiErrorAppend(errs, errors.New("template_name must not contain spaces"))
	}
	if c.FSStorage == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("filesystem_storage must be specified"))
	}
	if c.FSSize <= 0 {
		errs = packer.MultiErrorAppend(errs, errors.New("filesystem_size must be specified"))
	}

	if c.TemplateSuffix == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("template_suffix must be specified"))

	}

	if c.ProvisionIP == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("provision_ip must be specified"))
	}
	if c.ProvisionGatewayIP == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("provision_gateway_ip must be specified"))
	}

	// Set internal values
	//c.Comm.SSHAgentAuth = true

	c.Comm.SSHHost = c.ProvisionIP

	errs = packer.MultiErrorAppend(errs, c.Comm.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.BootConfig.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.HTTPConfig.Prepare(&c.ctx)...)

	if errs != nil && len(errs.Errors) > 0 {
		return nil, errs
	}

	packer.LogSecretFilter.Set(c.Password)
	return nil, nil
}
