package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	srvr := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	serverErr := srvr.ListenAndServe()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}
