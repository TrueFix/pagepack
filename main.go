package main

import (
	"log/slog"
	"os"

	"github.com/TrueFix/pagepack/pkg/config"
	"github.com/TrueFix/pagepack/pkg/logx"
	"github.com/TrueFix/pagepack/pkg/web"
)

func main() {
	c, err := config.Load()
	if err != nil {
		logx.New(logx.Options{}).Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	log := logx.New(logx.Options{Level: c.LogLevel, Format: c.LogFormat})
	slog.SetDefault(log)
	if err := web.StartServer(c, frontendFS(), log); err != nil {
		log.Error("pagepack stopped", "err", err)
		os.Exit(1)
	}
}
