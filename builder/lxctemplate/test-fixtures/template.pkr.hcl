source "proxmox-lxc-lxctemplate" "basic-example" {
  mock = "mock-config"
}

build {
  sources = [
    "source.proxmox-lxc-lxctemplate.basic-example"
  ]

  provisioner "shell-local" {
    inline = [
      "echo build generated data: ${build.GeneratedMockData}",
    ]
  }
}
