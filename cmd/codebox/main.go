package main

import (
	"log/slog"

	"github.com/plombardi89/codebox/internal/cli"
	"github.com/plombardi89/codebox/internal/logging"
	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/provider/azure"
	"github.com/plombardi89/codebox/internal/provider/hetzner"
)

func main() {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	log := logging.New(&levelVar)

	reg := provider.NewRegistry()
	reg.Register("azure", azure.New(log))
	reg.Register("hetzner", hetzner.New(log))

	cli.Execute(reg, log, &levelVar)
}
