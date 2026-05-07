package main

import (
	"flag"
	"log"
	"net/http"

	"bili-web-vps/internal/config"
	"bili-web-vps/internal/server"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	webRoot := flag.String("web", "web", "path to web/ directory (templates + static)")
	cacheRoot := flag.String("cache", "cache", "path to writable cache directory (used for HLS sessions)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv, err := server.New(cfg, *webRoot, *cacheRoot)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}

	log.Printf("bili-web listening on %s (config=%s, web=%s, cache=%s)", cfg.Listen, *cfgPath, *webRoot, *cacheRoot)
	if err := http.ListenAndServe(cfg.Listen, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
