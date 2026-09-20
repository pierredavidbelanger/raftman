package server

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/pierredavidbelanger/raftman/api"
)

//go:embed ui
var uiFS embed.FS

type HTTPConfig struct {
	Addr string
	Path string // URL prefix, used verbatim
	UI   bool   // serve the web UI at Path and the API under Path+"api/"
}

// HTTP serves the JSON API (stat, list) and optionally the web UI.
type HTTP struct {
	addr string
	srv  *http.Server
}

func NewHTTP(cfg HTTPConfig, store Store) (*HTTP, error) {
	mux := http.NewServeMux()
	apiPath := cfg.Path
	if cfg.UI {
		ui, err := fs.Sub(uiFS, "ui")
		if err != nil {
			return nil, err
		}
		mux.Handle(cfg.Path, http.FileServer(http.FS(ui)))
		apiPath = cfg.Path + "api/"
	}
	mux.HandleFunc(apiPath+"stat", query(store.QueryStat, func(msg string) *api.QueryStatResponse {
		return &api.QueryStatResponse{Error: msg}
	}))
	mux.HandleFunc(apiPath+"list", query(store.QueryList, func(msg string) *api.QueryListResponse {
		return &api.QueryListResponse{Error: msg}
	}))
	return &HTTP{addr: cfg.Addr, srv: &http.Server{Handler: mux}}, nil
}

func (h *HTTP) Start() error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return err
	}
	go h.srv.Serve(ln)
	return nil
}

// Close stops accepting connections and lets in-flight requests finish.
func (h *HTTP) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.srv.Shutdown(ctx)
}

// query builds a handler for one store method. Only POST bodies are decoded;
// any other method is an empty request. A failed query (timeout) answers 400
// with the error in the response body; SQL errors are already inside the
// response and answer 200.
func query[R any](run func(context.Context, *api.QueryRequest) (*R, error), failed func(string) *R) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := api.QueryRequest{}
		if r.Method == http.MethodPost {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		res, err := run(r.Context(), &req)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			res = failed(err.Error())
			w.WriteHeader(http.StatusBadRequest)
		}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			log.Printf("Unable to write response: %s", err)
		}
	}
}
