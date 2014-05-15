package app

import (
	"net/http"
)

func init() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/upload", uploadHandler)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {

}

func uploadHandler(w http.ResponseWriter, r *http.Request) {

}
