package main

import (
	"context"
	"log"
	"os"

	"foliospace-reader/internal/mcp"
)

func main() {
	log.SetOutput(os.Stderr)
	server := mcp.New(os.Getenv("FOLIOSPACE_BASE_URL"), os.Getenv("FOLIOSPACE_API_TOKEN"))
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
