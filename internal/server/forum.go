package server

import (
	"io"
	"net/http"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// registerForumRoutes wires the community forum and Avanza forum proxy.
func (e *Engine) registerForumRoutes() {
	e.mux.HandleFunc("POST /api/forum", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		post := storage.ForumPost{
			Author:    stringField(j, "author"),
			Title:     stringField(j, "title"),
			Content:   stringField(j, "content"),
			Symbol:    stringFieldOr(j, "symbol", ""),
			Timestamp: storage.NowSeconds(),
			Upvotes:   0,
		}

		id := e.db.CreateForumPost(post)
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "post_id": id})
	})

	e.mux.HandleFunc("GET /api/forum", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)

		symbol := queryParam(r, "symbol", "")
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := parseInt(v, 50); err == nil {
				limit = n
			}
		}

		includeAvanza := true
		switch r.URL.Query().Get("include_avanza") {
		case "false", "0":
			includeAvanza = false
		}

		jList := []map[string]any{}

		if includeAvanza && symbol != "" {
			for _, post := range market.FetchAvanzaForumPosts(symbol, limit) {
				jList = append(jList, post)
			}
		}

		for _, post := range e.db.GetForumPosts(symbol, limit) {
			jList = append(jList, map[string]any{
				"id":        post.ID,
				"author":    post.Author,
				"title":     post.Title,
				"content":   post.Content,
				"symbol":    post.Symbol,
				"timestamp": post.Timestamp,
				"upvotes":   post.Upvotes,
			})
		}

		writeJSON(w, http.StatusOK, jList)
	})

	e.mux.HandleFunc("POST /api/forum/vote", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		postID := int(floatField(j, "id"))
		delta := 1
		if v, ok := j["delta"].(float64); ok {
			delta = int(v)
		}

		if e.db.VoteForumPost(postID, delta) {
			writeRawJSON(w, http.StatusOK, `{"status":"success"}`)
		} else {
			writeRawJSON(w, http.StatusNotFound, `{"error":"Post not found"}`)
		}
	})
}
