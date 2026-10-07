package main

import (
	"bytes"
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
	"sort"
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
	client        *http.Client
	clientID      string
	clientSecret  string
	openRouterKey string
	openRouterURL string
	jevModel      string
	state         string
	verifier      string
}

type profile struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	Verified    bool   `json:"verified"`
	Metrics     struct {
		Followers int `json:"followers_count"`
	} `json:"public_metrics"`
}

type jevRequest struct {
	Instruction string   `json:"instruction"`
	IDs         []string `json:"ids"`
}

type jevMatch struct {
	ID     string  `json:"id"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score"`
}

type jevResult struct {
	Matches []jevMatch `json:"matches"`
	Cost    float64    `json:"cost"`
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

func (app *server) jev(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if app.openRouterKey == "" {
		http.Error(writer, "Set OPENROUTER_API_KEY in .env.local", http.StatusServiceUnavailable)
		return
	}
	var input jevRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil || strings.TrimSpace(input.Instruction) == "" || len(input.IDs) == 0 || len(input.IDs) > 150 {
		http.Error(writer, "Provide an instruction and 1–150 account IDs.", http.StatusBadRequest)
		return
	}
	contents, err := os.ReadFile("data/following.json")
	if err != nil {
		http.Error(writer, "Import your following list first.", http.StatusNotFound)
		return
	}
	var source snapshot
	if err := json.Unmarshal(contents, &source); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	wanted := map[string]bool{}
	for _, id := range input.IDs {
		wanted[id] = true
	}
	candidates := []profile{}
	for _, raw := range source.Following {
		var account profile
		if json.Unmarshal(raw, &account) == nil && wanted[account.ID] {
			candidates = append(candidates, account)
		}
	}
	questions := map[string]any{}
	for _, candidate := range candidates {
		questions[candidate.ID] = map[string]any{
			"type": "noul", "instructions": fmt.Sprintf("Does the account with ID %q match this instruction: %s", candidate.ID, input.Instruction),
			"criteria": map[string]string{"true": "The account matches the instruction from its provided profile.", "false": "The account does not match, or the profile lacks enough evidence."},
		}
	}
	body, _ := json.Marshal(map[string]any{"model": app.jevModel, "state": map[string]any{"accounts": candidates}, "questions": questions})
	endpoint := strings.TrimRight(strings.TrimSuffix(app.openRouterURL, "/v1"), "/") + "/alpha/decisions"
	apiRequest, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	apiRequest.Header.Set("Authorization", "Bearer "+app.openRouterKey)
	apiRequest.Header.Set("Content-Type", "application/json")
	response, err := app.client.Do(apiRequest)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		http.Error(writer, string(body), http.StatusBadGateway)
		return
	}
	var decision struct {
		Answers map[string]struct {
			Type string  `json:"type"`
			Noul float64 `json:"noul"`
		} `json:"answers"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decision); err != nil {
		http.Error(writer, "Jev returned invalid JSON", http.StatusBadGateway)
		return
	}
	result := jevResult{Matches: []jevMatch{}, Cost: decision.Usage.Cost}
	for id, answer := range decision.Answers {
		if answer.Type == "noul" && answer.Noul >= .5 {
			result.Matches = append(result.Matches, jevMatch{ID: id, Reason: fmt.Sprintf("Jev match %.0f%%", answer.Noul*100), Score: answer.Noul})
		}
	}
	sort.Slice(result.Matches, func(i, j int) bool { return result.Matches[i].Score > result.Matches[j].Score })
	allowed := map[string]bool{}
	for _, candidate := range candidates {
		allowed[candidate.ID] = true
	}
	filtered := result.Matches[:0]
	for _, match := range result.Matches {
		if allowed[match.ID] {
			filtered = append(filtered, match)
		}
	}
	result.Matches = filtered
	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(result)
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
	app := &server{
		client: http.DefaultClient, clientID: clientID, clientSecret: clientSecret,
		openRouterKey: os.Getenv("OPENROUTER_API_KEY"), openRouterURL: envOr("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"), jevModel: envOr("JEV_MODEL", "~typesafe/jev-latest"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", app.authorize)
	mux.HandleFunc("/auth/callback", app.callback)
	mux.HandleFunc("/api/following", following)
	mux.HandleFunc("/api/jev", app.jev)
	mux.Handle("/", http.FileServer(http.FS(subtree)))
	fmt.Println("Serving http://localhost:3000")
	return http.ListenAndServe(":3000", mux)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
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
