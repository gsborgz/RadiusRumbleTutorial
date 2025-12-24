package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"server/internal/server"
	"server/internal/server/clients"
)

var (
	port = flag.Int("port", 8080, "Port to listen on")
)

func main() {
	flag.Parse()

	// Game hub
	hub := server.NewHub()

	// Handler for websocket connections
	http.HandleFunc("/ws", func(writer http.ResponseWriter, request *http.Request) {
		hub.Serve(clients.NewWebsocketClient, writer, request)
	})

	go hub.Run()

	addr := fmt.Sprintf(":%d", *port)

	log.Printf("Starting server on %s", addr)

	err := http.ListenAndServe(addr, nil)

	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}