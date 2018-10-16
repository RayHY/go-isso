package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"gopkg.in/guregu/null.v3"

	"github.com/jinxiapu/go-isso/internal/pkg/db"
	"github.com/jinxiapu/go-isso/pkg/way"
)

func jSON(w http.ResponseWriter, v interface{}, status int) {
	// Set before WriteHeader cause https://golang.org/pkg/net/http/?#ResponseWriter
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.Encode(v)
}

func jsonError(w http.ResponseWriter, message string, status int) {
	jSON(w, map[string]string{"error": message}, status)
}

func (s *Server) handleStatusCode(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonError(w, http.StatusText(code), code)
	}
}

// TODO: params plain for plain text or contains html tag
func (s *Server) handleFetch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		RQuery := r.URL.Query()
		uris, ok := RQuery["uri"]
		if !ok {
			jsonError(w, "missing uri query", 400)
			return
		}
		uri := uris[0]
		// parse params
		var after float64
		var err error
		resultSlice, ok := RQuery["after"]
		if !ok {
			after = 0.01
		} else {
			after, err = strconv.ParseFloat(resultSlice[0], 64)
			if err != nil {
				jsonError(w, "param after invalid", 400)
				return
			}
		}

		parseURLParams2nullInts := func(w http.ResponseWriter, Q url.Values, keys []string, vars []*null.Int) error {
			for i, k := range keys {
				resultSlice, ok := Q[k]
				if !ok {
					continue
				}
				realV, err := strconv.Atoi(resultSlice[0])
				if err != nil || realV < 0 {
					jsonError(w, fmt.Sprintf("param %s invalid", k), 400)
					return errors.New("Invalid param")
				}
				*(vars[i]) = null.IntFrom(int64(realV))
			}
			return nil
		}

		var limit, parent, nestedLimit null.Int
		err = parseURLParams2nullInts(w, RQuery,
			[]string{"limit", "parent", "nested_limit"},
			[]*null.Int{&limit, &parent, &nestedLimit},
		)
		if err != nil {
			return
		}

		replyCounts, err := s.db.CountReply(uri, db.ModePublic, after)
		if err != nil {
			s.log.Errorln(err)
			http.Error(w, http.StatusText(500), 500)
			return
		}

		type reply struct {
			db.Comment
			HiddenReplies *int64  `json:"hidden_replies,omitempty"`
			TotalReplies  *int64  `json:"total_replies,omitempty"`
			Replies       []reply `json:"replies,omitempty"`
		}
		type FetchedComments struct {
			TotalReplies  int64    `json:"total_replies"`
			Replies       []reply  `json:"replies"`
			ID            null.Int `json:"id"`
			HiddenReplies int64    `json:"hidden_replies"`
		}

		_, ok = replyCounts[parent]
		if !ok {
			replyCounts[parent] = 0
		}

		var rJSON FetchedComments
		rJSON.ID = parent
		rJSON.TotalReplies = replyCounts[parent]

		FetchComments := func(w http.ResponseWriter, parent, limit null.Int) ([]reply, error) {
			comments, err := s.db.Fetch(uri, db.ModePublic, after, parent, "id", true, limit)
			if err != nil {
				s.log.Errorln(err)
				http.Error(w, http.StatusText(500), 500)
				return nil, err
			}
			replies := []reply{}
			for _, c := range comments {
				//TODO: need generate hash for comment email(or ip).
				//like this: c.Hash(methods string, meta) ! a methods for comment or anothor pkg.
				r := reply{c, nil, nil, []reply{}}
				replies = append(replies, r)
			}
			return replies, nil
		}

		rJSON.Replies, err = FetchComments(w, parent, limit)
		if err != nil {
			return
		}

		// run only when parent == NULL
		// I don't understand why but isso run like this.
		// so just keep compatible with isso api.
		if !parent.Valid {
			for i := range rJSON.Replies {
				rJSON.Replies[i].TotalReplies = new(int64)
				rJSON.Replies[i].HiddenReplies = new(int64)
				count, ok := replyCounts[null.IntFrom(rJSON.Replies[i].ID)]
				if !ok {
					*rJSON.Replies[i].TotalReplies = 0
				} else {
					*rJSON.Replies[i].TotalReplies = count
					rJSON.Replies[i].Replies, err = FetchComments(w, null.IntFrom(rJSON.Replies[i].ID), nestedLimit)
				}
				*rJSON.Replies[i].HiddenReplies = *rJSON.Replies[i].TotalReplies - int64(len(rJSON.Replies[i].Replies))
			}
		}

		rJSON.HiddenReplies = replyCounts[parent] - int64(len(rJSON.Replies))
		jSON(w, rJSON, 200)
	}
}

func (s *Server) handleNew() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uris, ok := r.URL.Query()["uri"]
		if !ok {
			jsonError(w, "missing uri query", 400)
			return
		}
		uri := uris[0]
		var nc struct {
			Text         string      `json:"text"`
			Parent       null.Int   `json:"parent"`
			Author       null.String `json:"author"`
			Email        null.String `json:"email"`
			Website      null.String `json:"website"`
		}
		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(1<<14))).Decode(&nc)
		if err != nil {
			jsonError(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		s.log.Debugln(uri)
	}
}

func (s *Server) handleHello() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := way.Param(r.Context(), "name")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, fmt.Sprintf("hello, %s", name))
	}
}