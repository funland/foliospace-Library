package main

import (
	"log"
	"net/http"

	"foliospace-reader/internal/config"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/httpapi"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func main() {
	cfg := config.Load()

	conn, err := db.Open(cfg.ConfigDir)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	appStore := store.New(conn)
	if _, err := appStore.CreateLibrary("Library", cfg.LibraryDir); err != nil {
		log.Fatal(err)
	}

	api := httpapi.NewWithOptions(service.New(appStore), http.FileServer(http.Dir("web/dist")), httpapi.Options{APIToken: cfg.APIToken})

	log.Printf("FolioSpace Library listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, api.Routes()); err != nil {
		log.Fatal(err)
	}
}
