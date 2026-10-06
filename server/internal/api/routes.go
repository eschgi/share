package api

import (
	"net/http"
	"strings"

	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
)

// routes registers the API's handlers. Every wildcard in the API's paths is an id, so a
// request whose wildcard isn't one gets a 404, as an id nothing has does, before its handler
// asks the database, which would refuse the text as a uuid.
type routes struct{ mux *http.ServeMux }

func (rs routes) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	names := wildcards(pattern)
	if len(names) == 0 {
		rs.mux.HandleFunc(pattern, h)
		return
	}
	rs.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		for _, name := range names {
			if !ids.Valid(r.PathValue(name)) {
				httpx.WriteError(w, http.StatusNotFound, "not_found", "Nothing has that id.")
				return
			}
		}
		h(w, r)
	})
}

// wildcards lists the names of the wildcards in a ServeMux pattern: id and user in
// "PUT /api/folders/{id}/people/{user}".
func wildcards(pattern string) []string {
	var names []string
	for _, segment := range strings.Split(pattern, "/") {
		if name, ok := strings.CutPrefix(segment, "{"); ok {
			names = append(names, strings.TrimSuffix(strings.TrimSuffix(name, "}"), "..."))
		}
	}
	return names
}
