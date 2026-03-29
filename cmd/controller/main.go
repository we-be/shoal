package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/we-be/shoal/internal/controller"
)

func main() {
	addr := flag.String("addr", ":8180", "address to listen on")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("shoal controller listening on %s", *addr)

	srv := controller.NewServer()
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatalf("controller died: %v", err)
	}
}
