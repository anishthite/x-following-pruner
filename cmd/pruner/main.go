package main

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
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
const redirectURL = "http://localhost:3000/auth/callback"
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

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type server struct {
	client       *http.Client
	clientID     string
	clientSecret string
	state        string
	verifier     string
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
	return fetchFollowingForOwner(client, baseURL, token, ownerResponse.Data)
}

func fetchFollowingForOwner(client *http.Client, baseURL, token string, ownerData json.RawMessage) (snapshot, error) {
	var account owner
	if len(ownerData) == 0 || string(ownerData) == "null" || json.Unmarshal(ownerData, &account) != nil || account.ID == "" {
		return snapshot{}, errors.New("X account was not found")
	}

	result := snapshot{FetchedAt: time.Now().UTC(), Owner: ownerData}
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

func randomURLString() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (app *server) authorize(writer http.ResponseWriter, request *http.Request) {
	state, err := randomURLString()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	verifier, err := randomURLString()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	app.state, app.verifier = state, verifier
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {app.clientID},
		"redirect_uri":          {redirectURL},
		"scope":                 {"tweet.read users.read follows.read"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(writer, request, "https://x.com/i/oauth2/authorize?"+query.Encode(), http.StatusFound)
}

func (app *server) callback(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("state") != app.state || app.state == "" || request.URL.Query().Get("code") == "" {
		http.Error(writer, "Invalid X authorization response", http.StatusBadRequest)
		return
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {request.URL.Query().Get("code")},
		"redirect_uri":  {redirectURL},
		"code_verifier": {app.verifier},
	}
	tokenRequest, err := http.NewRequest(http.MethodPost, apiURL+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRequest.SetBasicAuth(app.clientID, app.clientSecret)
	authResponse, err := app.client.Do(tokenRequest)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	defer authResponse.Body.Close()
	if authResponse.StatusCode < 200 || authResponse.StatusCode >= 300 {
		body, _ := io.ReadAll(authResponse.Body)
		http.Error(writer, fmt.Sprintf("X OAuth %s: %s", authResponse.Status, body), http.StatusBadGateway)
		return
	}
	var token tokenResponse
	if err := json.NewDecoder(authResponse.Body).Decode(&token); err != nil || token.AccessToken == "" {
		http.Error(writer, "X did not return an access token", http.StatusBadGateway)
		return
	}
	var me response
	if err := get(app.client, apiURL+"/users/me?user.fields=username", token.AccessToken, &me); err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	value, err := fetchFollowingForOwner(app.client, apiURL, token.AccessToken, me.Data)
	if err == nil {
		err = writeSnapshot(value, "data/following.json")
	}
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	app.state, app.verifier = "", ""
	http.Redirect(writer, request, "/?imported="+fmt.Sprint(len(value.Following)), http.StatusFound)
}

func following(writer http.ResponseWriter, _ *http.Request) {
	contents, err := os.ReadFile("data/following.json")
	if errors.Is(err, os.ErrNotExist) {
		http.Error(writer, "Import your following list first.", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Write(contents)
}

func serve() error {
	if err := loadEnvFile(".env.local"); err != nil {
		return err
	}
	clientID, err := requiredEnv("X_CLIENT_ID")
	if err != nil {
		return err
	}
	clientSecret, err := requiredEnv("X_CLIENT_SECRET")
	if err != nil {
		return err
	}
	subtree, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	app := &server{client: http.DefaultClient, clientID: clientID, clientSecret: clientSecret}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", app.authorize)
	mux.HandleFunc("/auth/callback", app.callback)
	mux.HandleFunc("/api/following", following)
	mux.Handle("/", http.FileServer(http.FS(subtree)))
	fmt.Println("Serving http://localhost:3000")
	return http.ListenAndServe(":3000", mux)
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: pruner serve")
		os.Exit(1)
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
