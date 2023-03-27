source "proxmox-lxc-vztmpl" "basic-example" {
  mock = "mock-config"
}

build {
  sources = [
    "source.proxmox-lxc-vztmpl.basic-example"
  ]

  provisioner "shell-local" {
    inline = [
      "echo build generated data: ${build.GeneratedMockData}",
    ]
  }
}
