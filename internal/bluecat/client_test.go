package bluecat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginAndListZones(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/sessions", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"apiToken": "tok", "username": "api"})
	})
	mux.HandleFunc("/api/v2/zones", func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.Header.Get("Authorization"), "Basic ")
		require.Contains(t, r.URL.Query().Get("filter"), "example.com")
		_ = json.NewEncoder(w).Encode(collection[Zone]{
			Data: []Zone{{ID: ptr(int64(7)), AbsoluteName: ptr("example.com")}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := Login(context.Background(), Config{Host: srv.URL, Username: "api", Password: "secret"})
	require.NoError(t, err)

	zones, err := client.ListZones(context.Background(), "example.com")
	require.NoError(t, err)
	require.Len(t, zones, 1)
	require.Equal(t, "example.com", zones[0].AbsoluteNameOrEmpty())
}

func TestCreateHostUsesRelativeName(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"apiToken": "tok"})
	})
	mux.HandleFunc("/api/v2/zones/7/resourceRecords", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &got))
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := Login(context.Background(), Config{Host: srv.URL, Username: "api", Password: "secret"})
	require.NoError(t, err)

	err = client.CreateOrUpdateHost(context.Background(), Zone{ID: ptr(int64(7)), AbsoluteName: ptr("example.com")}, HostRecord{
		AbsoluteName: ptr("app.example.com"),
		Addresses:    []Address{{Address: ptr("192.0.2.10")}},
	})
	require.NoError(t, err)
	require.Equal(t, "HostRecord", got["type"])
	require.Equal(t, "app", got["name"])
	require.Nil(t, got["absoluteName"])
}

func TestRelativeName(t *testing.T) {
	require.Equal(t, "app", relativeName("app.example.com", "example.com"))
	require.Equal(t, "", relativeName("example.com", "example.com"))
}
