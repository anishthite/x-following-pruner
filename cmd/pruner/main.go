package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiURL = "https://api.x.com/2"
const userFields = "created_at,description,public_metrics,profile_image_url,verified"

//go:embed web/*
var webFiles embed.FS

type response struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		NextToken string `json:"next_token"`
	} `json:"meta"`
}

type owner struct {
	ID string `json:"id"`
}

type snapshot struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Owner     json.RawMessage   `json:"owner"`
	Following []json.RawMessage `json:"following"`
}

func get(client *http.Client, endpoint, token string, target any) error {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("X API %s: %s", response.Status, body)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func fetchFollowing(client *http.Client, baseURL, token, username string) (snapshot, error) {
	username = strings.TrimPrefix(username, "@")
	var ownerResponse response
	if err := get(client, baseURL+"/users/by/username/"+url.PathEscape(username), token, &ownerResponse); err != nil {
		return snapshot{}, err
	}
	var account owner
	if len(ownerResponse.Data) == 0 || string(ownerResponse.Data) == "null" || json.Unmarshal(ownerResponse.Data, &account) != nil || account.ID == "" {
		return snapshot{}, fmt.Errorf("X user @%s was not found", username)
	}

	result := snapshot{FetchedAt: time.Now().UTC(), Owner: ownerResponse.Data}
	for nextToken := ""; ; {
		query := url.Values{"max_results": {"1000"}, "user.fields": {userFields}}
		if nextToken != "" {
			query.Set("pagination_token", nextToken)
		}
		var page response
		if err := get(client, baseURL+"/users/"+account.ID+"/following?"+query.Encode(), token, &page); err != nil {
			return snapshot{}, err
		}
		if len(page.Data) > 0 && string(page.Data) != "null" {
			var users []json.RawMessage
			if err := json.Unmarshal(page.Data, &users); err != nil {
				return snapshot{}, err
			}
			result.Following = append(result.Following, users...)
		}
		if page.Meta.NextToken == "" {
			return result, nil
		}
		nextToken = page.Meta.NextToken
	}
}

func writeSnapshot(value snapshot, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".following-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func loadEnvFile(path string) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("missing %s; copy .env.example to .env.local", path)
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(key, "#") && os.Getenv(key) == "" {
			os.Setenv(key, strings.TrimSpace(value))
		}
	}
	return nil
}

func requiredEnv(name string) (string, error) {
	if value := os.Getenv(name); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("missing %s", name)
}

func serve() error {
	subtree, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	fmt.Println("Serving http://localhost:3000")
	return http.ListenAndServe(":3000", http.FileServer(http.FS(subtree)))
}

func runImport() error {
	if err := loadEnvFile(".env.local"); err != nil {
		return err
	}
	token, err := requiredEnv("X_BEARER_TOKEN")
	if err != nil {
		return err
	}
	username, err := requiredEnv("X_USERNAME")
	if err != nil {
		return err
	}
	value, err := fetchFollowing(http.DefaultClient, apiURL, token, username)
	if err != nil {
		return err
	}
	path := "data/following.json"
	if err := writeSnapshot(value, path); err != nil {
		return err
	}
	fmt.Printf("Saved %d accounts to %s\n", len(value.Following), path)
	return nil
}

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "serve":
		err = serve()
	case len(os.Args) == 2 && os.Args[1] == "fetch-following":
		err = runImport()
	default:
		err = fmt.Errorf("usage: pruner serve | pruner fetch-following")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
