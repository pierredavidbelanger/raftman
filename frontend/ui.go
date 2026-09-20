package frontend

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"

	"github.com/pierredavidbelanger/raftman/spi"
)

//go:embed static/ui
var staticFS embed.FS

type uiFrontend struct {
	webFrontend
	api *apiFrontend
}

func newUIFrontend(e spi.LogEngine, frontendURL *url.URL) (*uiFrontend, error) {
	f := uiFrontend{}
	if err := initWebFrontend(e, frontendURL, &f.webFrontend); err != nil {
		return nil, err
	}
	f.api = &apiFrontend{}
	return &f, nil
}

func (f *uiFrontend) Start() error {
	_, b := f.e.GetBackend()
	f.api.b = b
	ui, err := fs.Sub(staticFS, "static/ui")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(f.path+"api/stat", f.api.handleStat)
	mux.HandleFunc(f.path+"api/list", f.api.handleList)
	mux.Handle(f.path, http.FileServer(http.FS(ui)))
	return f.startHandler(mux)
}

func (f *uiFrontend) Close() error {
	return f.close()
}
