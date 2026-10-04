package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchFollowingPaginates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatal("missing bearer token")
		}
		switch request.URL.Path {
		case "/2/users/by/username/anish":
			writer.Write([]byte(`{"data":{"id":"owner","username":"anish"}}`))
		case "/2/users/owner/following":
			if request.URL.Query().Get("pagination_token") == "next" {
				writer.Write([]byte(`{"data":[{"id":"2","username":"two"}],"meta":{}}`))
				return
			}
			if request.URL.Query().Get("max_results") != "1000" {
				t.Fatal("expected max_results=1000")
			}
			writer.Write([]byte(`{"data":[{"id":"1","username":"one"}],"meta":{"next_token":"next"}}`))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	value, err := fetchFollowing(server.Client(), server.URL+"/2", "token", "@anish")
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Following) != 2 || !strings.Contains(string(value.Following[1]), `"id":"2"`) {
		t.Fatalf("unexpected following: %s", value.Following)
	}
}
