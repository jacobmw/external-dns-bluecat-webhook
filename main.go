package main

import (
	"fmt"
	"net/http"
	"os"
)

func negotiate(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fmt.Fprintln(w, "Negotiate")
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func records(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fmt.Fprintln(w, "Get Records")
	} else if r.Method == "POST" {
		fmt.Fprintln(w, "Apply Changes")
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func adjustendpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		fmt.Fprintln(w, "Adjust Endpoints")
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// From https://github.com/kubernetes-sigs/external-dns/blob/ea6d44b0d81c635ee275b51ab777a17151a08e9d/provider/webhook/api/httpapi.go#L112
// The server will respond to the following endpoints:
// - / (GET): initialization, negotiates headers and returns the domain filter
// - /records (GET): returns the current records
// - /records (POST): applies the changes
// - /adjustendpoints (POST): executes the AdjustEndpoints method
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", negotiate)
	mux.HandleFunc("/records", records)
	mux.HandleFunc("/adjustendpoints", adjustendpoints)

	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
