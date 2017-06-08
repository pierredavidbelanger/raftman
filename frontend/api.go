package frontend

import (
	"github.com/pierredavidbelanger/raftman/spi"
	"net/url"
	"net/http"
	"github.com/pierredavidbelanger/raftman/api"
	"encoding/json"
)

type apiFrontend struct {
	webFrontend
}

func newAPIFrontend(e spi.LogEngine, frontendURL *url.URL) (*apiFrontend, error) {
	f := apiFrontend{}
	if err := initWebFrontend(e, frontendURL, &f.webFrontend); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *apiFrontend) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc(f.path+"stat", f.handleStat)
	mux.HandleFunc(f.path+"list", f.handleList)
	return f.startHandler(mux)
}

func (f *apiFrontend) Close() error {
	return f.close()
}

func (f *apiFrontend) handleStat(w http.ResponseWriter, r *http.Request) {

	req := api.QueryRequest{}

	if r.Method == "POST" {
		defer r.Body.Close()
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}

	res, err := f.b.QueryStat(&req)
	if err != nil {
		res = &api.QueryStatResponse{Error: err.Error()}
		w.WriteHeader(400)
	}

	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}

func (f *apiFrontend) handleList(w http.ResponseWriter, r *http.Request) {

	req := api.QueryRequest{}

	if r.Method == "POST" {
		defer r.Body.Close()
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}

	res, err := f.b.QueryList(&req)
	if err != nil {
		res = &api.QueryListResponse{Error: err.Error()}
		w.WriteHeader(400)
	}

	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}
