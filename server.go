package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	users = make(map[string]User)
	sessions = make(map[string]Session)

	r := mux.NewRouter()

	r.HandleFunc("/api/auth", SessionMiddleware(HandleLogin, false)).Methods("POST")
	r.HandleFunc("/api/register", SessionMiddleware(HandleRegister, false)).Methods("POST")
	r.HandleFunc("/api/upload_avatar", SessionMiddleware(HandleAvatarUpload, true)).Methods("POST")
	r.HandleFunc("/api/profile", SessionMiddleware(HandleUpdateUser, true)).Methods("PUT")
	r.HandleFunc("/api/profile", SessionMiddleware(HandleGetUserData, true)).Methods("GET")
	r.HandleFunc("/api/leaderbord/{page:[0-9]+}", SessionMiddleware(HandleGetUsers, true)).Methods("GET")
	staticServer := http.FileServer(http.Dir("static/"))
	mediaServer := http.FileServer(http.Dir("media/"))
	r.PathPrefix("/static").Handler(http.StripPrefix("/static/", staticServer))
	r.PathPrefix("/media").Handler(http.StripPrefix("/media/", mediaServer))

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	log.Fatal(http.ListenAndServe(":8081", r))
}
