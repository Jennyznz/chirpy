package main

import "net/http"

func main() {
	serveMux := http.NewServeMux()
	fullDir := http.Dir(".")
	serveMux.Handle("/", http.FileServer(fullDir))

	server := &http.Server {
		Handler: serveMux,
		Addr: ":8080",
	}

	server.ListenAndServe()
}


