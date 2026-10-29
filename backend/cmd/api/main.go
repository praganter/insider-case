package main

import (
	"log"
	"net/http"
	"os"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	return mux
}

func main() {
	addr := ":" + os.Getenv("API_PORT")
	if addr == ":" {
		addr = ":8080"
	}
	log.Printf("api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, newMux()))
}
