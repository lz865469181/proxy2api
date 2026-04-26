package main

import (
	"flag"
	"log"

	"proxy2api/internal/config"
	"proxy2api/internal/gateway"
	"proxy2api/internal/store"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	st, err := store.Open(cfg.DB.Path)
	if err != nil {
		log.Fatalf("open db failed: %v", err)
	}
	defer st.Close()

	if err := st.SeedFromConfig(cfg); err != nil {
		log.Fatalf("seed db failed: %v", err)
	}

	server, err := gateway.NewServer(cfg, st)
	if err != nil {
		log.Fatalf("init server failed: %v", err)
	}

	log.Printf("proxy2api listening on %s", cfg.Server.Listen)
	if err := server.Start(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
