package main

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"foliospace-reader/internal/config"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/httpapi"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func main() {
	cfg := config.Load()
	memoryLimit := int64(cfg.MemoryLimitMB) << 20
	debug.SetMemoryLimit(memoryLimit)
	log.Printf("Go memory limit set to %d MiB", cfg.MemoryLimitMB)

	conn, err := db.Open(cfg.ConfigDir)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	appStore := store.New(conn)
	if count, err := appStore.CancelInterruptedScanJobs(); err != nil {
		log.Printf("failed to mark interrupted scan jobs: %v", err)
	} else if count > 0 {
		log.Printf("marked %d interrupted scan job(s) as cancelled", count)
	}

	api := httpapi.NewWithOptions(service.NewWithConfig(appStore, cfg.ConfigDir), http.FileServer(http.Dir("web/dist")), httpapi.Options{APIToken: cfg.APIToken})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("FolioSpace Library listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
