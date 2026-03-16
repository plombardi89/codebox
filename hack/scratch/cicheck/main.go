package main

import (
	"fmt"

	"github.com/voidfunktion/ocbox/internal/cloudinit"
)

func main() {
	cfg := cloudinit.Config{
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyHere user@host",
	}
	out, err := cloudinit.Generate(cfg)
	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}
	fmt.Print(out)
}
